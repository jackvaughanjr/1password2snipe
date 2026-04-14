package sync

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackvaughanjr/1password2snipe/internal/onepassword"
	"github.com/jackvaughanjr/1password2snipe/internal/snipeit"
)

// Config controls sync behaviour.
type Config struct {
	DryRun        bool
	Force         bool
	CreateUsers   bool
	IncludeGuests bool
	LicenseName       string
	LicenseCategoryID int
	// ManufacturerID is optional. If 0, auto find/create "1Password".
	ManufacturerID int
	// SupplierID is optional. If 0, no supplier is set on the license.
	SupplierID int
}

// Syncer orchestrates the 1Password → Snipe-IT license sync.
type Syncer struct {
	op    *onepassword.Client
	snipe *snipeit.Client
	cfg   Config
}

func NewSyncer(op *onepassword.Client, snipe *snipeit.Client, cfg Config) *Syncer {
	return &Syncer{op: op, snipe: snipe, cfg: cfg}
}

// Run executes the full sync. emailFilter restricts the checkout pass to one
// user (and skips the checkin pass entirely).
func (s *Syncer) Run(ctx context.Context, emailFilter string) (Result, error) {
	var result Result

	// 1. Fetch all active members from 1Password SCIM bridge.
	slog.Info("fetching active 1Password members")
	allActive, err := s.op.ListActiveUsers(ctx)
	if err != nil {
		return result, fmt.Errorf("listing active 1Password users: %w", err)
	}
	slog.Info("fetched active members", "count", len(allActive))

	// 2. Filter guests if not included.
	activeUsers := allActive
	if !s.cfg.IncludeGuests {
		filtered := activeUsers[:0]
		for _, u := range allActive {
			if !isGuest(u) {
				filtered = append(filtered, u)
			}
		}
		activeUsers = filtered
		slog.Info("filtered guest members", "remaining", len(activeUsers))
	}

	// 3. Build active email set for the checkin pass.
	activeEmails := make(map[string]struct{}, len(activeUsers))
	for _, u := range activeUsers {
		activeEmails[emailKey(u)] = struct{}{}
	}

	// 4. Apply --email filter.
	if emailFilter != "" {
		needle := strings.ToLower(emailFilter)
		filtered := activeUsers[:0]
		for _, u := range activeUsers {
			if emailKey(u) == needle {
				filtered = append(filtered, u)
				break
			}
		}
		activeUsers = filtered
		slog.Info("filtered to single user", "email", emailFilter, "found", len(activeUsers) > 0)
	}

	// 5. Resolve manufacturer — find or create "1Password" in Snipe-IT.
	manufacturerID := s.cfg.ManufacturerID
	if !s.cfg.DryRun && manufacturerID == 0 {
		mfr, err := s.snipe.FindOrCreateManufacturer(ctx, "1Password", "https://1password.com")
		if err != nil {
			return result, fmt.Errorf("resolving manufacturer: %w", err)
		}
		manufacturerID = mfr.ID
	}

	// 6. Find or create the license.
	// Dry-run: find only; synthesize placeholder if not found (id=0).
	slog.Info("finding or creating license", "name", s.cfg.LicenseName)
	var lic *snipeit.License
	activeCount := len(activeEmails)
	if s.cfg.DryRun {
		lic, err = s.snipe.FindLicenseByName(ctx, s.cfg.LicenseName)
		if err != nil {
			return result, err
		}
		if lic == nil {
			slog.Info("[dry-run] license not found; would be created", "name", s.cfg.LicenseName, "seats", activeCount)
			lic = &snipeit.License{Name: s.cfg.LicenseName, Seats: activeCount}
		}
	} else {
		lic, err = s.snipe.FindOrCreateLicense(ctx, s.cfg.LicenseName, activeCount, s.cfg.LicenseCategoryID, manufacturerID, s.cfg.SupplierID)
		if err != nil {
			return result, err
		}
	}
	slog.Info("license resolved", "id", lic.ID, "seats", lic.Seats, "free", lic.FreeSeatsCount)

	// 7. Expand seats if needed (never shrink automatically).
	if activeCount > lic.Seats {
		slog.Info("expanding license seats", "current", lic.Seats, "needed", activeCount)
		if !s.cfg.DryRun {
			lic, err = s.snipe.UpdateLicenseSeats(ctx, lic.ID, activeCount)
			if err != nil {
				return result, err
			}
		}
	}

	// 8. Load current seat assignments.
	// Dry-run with a synthetic license (id=0) skips the API call.
	// In production, id=0 means something went wrong — fail fast.
	checkedOutByEmail := make(map[string]*snipeit.LicenseSeat)
	var freeSeats []*snipeit.LicenseSeat
	if lic.ID != 0 {
		slog.Info("loading current seat assignments")
		seats, err := s.snipe.ListLicenseSeats(ctx, lic.ID)
		if err != nil {
			return result, err
		}
		for i := range seats {
			seat := &seats[i]
			if seat.AssignedTo != nil && seat.AssignedTo.Email != "" {
				checkedOutByEmail[strings.ToLower(seat.AssignedTo.Email)] = seat
			} else {
				freeSeats = append(freeSeats, seat)
			}
		}

		// Ghost cleanup: seats Snipe-IT counts as used but have no assigned_user.
		snipeCheckedOut := lic.Seats - lic.FreeSeatsCount
		ghostCount := snipeCheckedOut - len(checkedOutByEmail)
		if ghostCount > 0 {
			slog.Info("cleaning up ghost checkouts", "count", ghostCount)
			cleaned := 0
			for cleaned < ghostCount && len(freeSeats) > 0 {
				ghost := freeSeats[0]
				freeSeats = freeSeats[1:]
				if !s.cfg.DryRun {
					if err := s.snipe.CheckinSeat(ctx, lic.ID, ghost.ID); err != nil {
						slog.Warn("failed to clean up ghost seat", "seat_id", ghost.ID, "error", err)
						freeSeats = append(freeSeats, ghost)
						break
					}
				} else {
					slog.Info("[dry-run] would clean up ghost seat", "seat_id", ghost.ID)
				}
				cleaned++
			}
		}
	} else if !s.cfg.DryRun {
		return result, fmt.Errorf("license resolved with id=0 in production mode — check Snipe-IT API permissions and required fields")
	} else {
		slog.Info("[dry-run] skipping seat load for new license")
	}
	slog.Info("seat state loaded", "checked_out", len(checkedOutByEmail), "free", len(freeSeats))

	// 9. Checkout / update loop.
	for _, u := range activeUsers {
		email := emailKey(u)
		notes := buildNotes(u)

		snipeUser, err := s.snipe.FindUserByEmail(ctx, email)
		if err != nil {
			slog.Warn("error looking up Snipe-IT user", "email", email, "error", err)
			result.Warnings++
			continue
		}
		if snipeUser == nil {
			if !s.cfg.CreateUsers {
				slog.Warn("no Snipe-IT user found for 1Password member", "email", email)
				result.UnmatchedEmails = append(result.UnmatchedEmails, email)
				result.Warnings++
				continue
			}
			// Derive first/last name from the SCIM name struct.
			firstName := email
			lastName := ""
			if u.Name != nil {
				if u.Name.GivenName != "" {
					firstName = u.Name.GivenName
				}
				if u.Name.FamilyName != "" {
					lastName = u.Name.FamilyName
				}
			}
			if s.cfg.DryRun {
				slog.Info("[dry-run] would create Snipe-IT user", "email", email)
				result.UsersCreated++
				result.CheckedOut++
				continue
			}
			createNotes := "Auto-created from 1Password Business via 1password2snipe"
			created, err := s.snipe.CreateUser(ctx, firstName, lastName, email, email, createNotes, "")
			if err != nil {
				slog.Warn("failed to create Snipe-IT user", "email", email, "error", err)
				result.Warnings++
				continue
			}
			snipeUser = created
			result.UsersCreated++
		}

		if existing, ok := checkedOutByEmail[email]; ok {
			if existing.Notes == notes && !s.cfg.Force {
				slog.Debug("seat up to date", "email", email)
				result.Skipped++
				continue
			}
			slog.Info("updating seat notes", "email", email, "dry_run", s.cfg.DryRun)
			if !s.cfg.DryRun {
				if err := s.snipe.UpdateSeatNotes(ctx, lic.ID, existing.ID, notes); err != nil {
					slog.Warn("failed to update seat notes", "email", email, "error", err)
					result.Warnings++
					continue
				}
			}
			result.NotesUpdated++
			continue
		}

		if s.cfg.DryRun {
			slog.Info("[dry-run] would check out seat", "email", email, "notes", notes)
			result.CheckedOut++
			continue
		}
		if len(freeSeats) == 0 {
			slog.Warn("no free seats available", "email", email)
			result.Warnings++
			continue
		}
		seat := freeSeats[0]
		freeSeats = freeSeats[1:]

		slog.Info("checking out seat", "email", email, "seat_id", seat.ID)
		if err := s.snipe.CheckoutSeat(ctx, lic.ID, seat.ID, snipeUser.ID, notes); err != nil {
			slog.Warn("failed to checkout seat", "email", email, "error", err)
			freeSeats = append(freeSeats, seat) // return seat on failure
			result.Warnings++
			continue
		}
		result.CheckedOut++
	}

	// 10. Checkin pass — skip when --email filter is set.
	if emailFilter == "" {
		for email, seat := range checkedOutByEmail {
			if _, active := activeEmails[email]; active {
				continue
			}
			slog.Info("checking in seat for inactive member", "email", email, "seat_id", seat.ID, "dry_run", s.cfg.DryRun)
			if !s.cfg.DryRun {
				if err := s.snipe.CheckinSeat(ctx, lic.ID, seat.ID); err != nil {
					slog.Warn("failed to checkin seat", "email", email, "error", err)
					result.Warnings++
					continue
				}
			}
			result.CheckedIn++
		}
	}

	return result, nil
}

// emailKey returns the canonical (lowercased) email for a 1Password user.
// The SCIM userName field is the primary email address.
func emailKey(u onepassword.User) string {
	return strings.ToLower(u.UserName)
}

// isGuest reports whether the user has the Guest role in 1Password.
func isGuest(u onepassword.User) bool {
	for _, r := range u.Roles {
		if strings.EqualFold(r.Value, "GUEST") {
			return true
		}
	}
	return false
}

// buildNotes returns a formatted string of the user's 1Password roles for
// storage in the Snipe-IT seat notes field. Roles are sorted alphabetically.
// Returns an empty string if the user has no roles.
func buildNotes(u onepassword.User) string {
	if len(u.Roles) == 0 {
		return ""
	}
	labels := make([]string, 0, len(u.Roles))
	for _, r := range u.Roles {
		label := r.Display
		if label == "" {
			label = r.Value
		}
		if label != "" {
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		return ""
	}
	sort.Strings(labels)
	return "roles: " + strings.Join(labels, ", ")
}

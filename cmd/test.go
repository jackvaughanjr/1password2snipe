package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackvaughanjr/1password2snipe/internal/onepassword"
	"github.com/jackvaughanjr/1password2snipe/internal/snipeit"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Validate API connections and report current state",
	RunE:  runTest,
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	opURL := viper.GetString("onepassword.url")
	if opURL == "" {
		return fatal("onepassword.url is required in settings.yaml (or OP_SCIM_URL env var)")
	}
	opToken := viper.GetString("onepassword.api_token")
	if opToken == "" {
		return fatal("onepassword.api_token is required in settings.yaml (or OP_SCIM_TOKEN env var)")
	}
	snipeURL := viper.GetString("snipe_it.url")
	if snipeURL == "" {
		return fatal("snipe_it.url is required in settings.yaml (or SNIPE_URL env var)")
	}
	snipeKey := viper.GetString("snipe_it.api_key")
	if snipeKey == "" {
		return fatal("snipe_it.api_key is required in settings.yaml (or SNIPE_TOKEN env var)")
	}

	opClient := onepassword.NewClient(opURL, opToken)
	rateLimitMs := viper.GetInt("sync.rate_limit_ms")
	if rateLimitMs <= 0 {
		rateLimitMs = 500
	}
	snipeClient := snipeit.NewClient(snipeURL, snipeKey, rateLimitMs)

	ctx := context.Background()

	// --- 1Password SCIM Bridge ---
	fmt.Println("=== 1Password ===")
	fmt.Printf("SCIM bridge: %s\n", opURL)

	if err := opClient.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "1Password SCIM error: %v\n", err)
		return err
	}
	fmt.Println("Connection: OK")

	users, err := opClient.ListActiveUsers(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "1Password SCIM error: %v\n", err)
		return err
	}
	fmt.Printf("Active members: %d\n", len(users))

	// Count by role
	roleCounts := make(map[string]int)
	for _, u := range users {
		for _, r := range u.Roles {
			label := r.Display
			if label == "" {
				label = r.Value
			}
			roleCounts[label]++
		}
		if len(u.Roles) == 0 {
			roleCounts["(no role)"]++
		}
	}
	for role, count := range roleCounts {
		fmt.Printf("  %-20s %d\n", role+":", count)
	}

	// --- Snipe-IT ---
	fmt.Println("\n=== Snipe-IT ===")
	licenseName := viper.GetString("snipe_it.license_name")
	if licenseName == "" {
		licenseName = "1Password Business"
	}

	lic, err := snipeClient.FindLicenseByName(ctx, licenseName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Snipe-IT error: %v\n", err)
		return err
	}
	if lic == nil {
		fmt.Printf("License %q: not found (will be created on first sync)\n", licenseName)
	} else {
		fmt.Printf("License %q: id=%d seats=%d free=%d\n",
			lic.Name, lic.ID, lic.Seats, lic.FreeSeatsCount)
	}

	return nil
}

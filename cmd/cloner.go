package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"sra/vat"
	"sra/vat/internal/util"

	"github.com/spf13/cobra"
)

// Dedicated flag-backing vars for the clone command. These intentionally do NOT
// reuse the shared package-level vars in cmd.go/saver.go: every command's init()
// runs at startup, so binding a clone flag to a shared var would let another
// command's init() silently overwrite clone's default.
var (
	cloneHostname             string
	cloneCredentialsFile      string
	cloneSourceDB             string
	cloneTargetDB             string
	cloneAssessmentName       string
	cloneTargetAssessmentName string
	cloneOverrideTemplate     bool
	cloneDeleteOnFailure      bool
	cloneForceEnvOnly         bool
	cloneSourceCampaignName   string
)

// ErrCloneOntoItself is returned when a clone would land on top of the very
// assessment it is copying, i.e. the same database and the same name.
var ErrCloneOntoItself = errors.New("clone target is identical to the source")

// resolveCloneTarget resolves the effective target database for a clone and
// rejects a clone that would land on its own source. An unset --target-db means
// "clone within the source database", in which case the copy must be given a
// different assessment name; cloning into a different database may reuse the name.
//
// campaignOnly relaxes the identity check: in campaign-only mode
// --target-assessment-name names an *existing* assessment that receives a copy of
// the campaign, so naming the source assessment is the legitimate way to spell
// "duplicate this campaign inside its own assessment". Callers are expected to
// pass already-trimmed values; the inputs are trimmed defensively anyway.
func resolveCloneTarget(sourceDB, targetDB, assessmentName, targetAssessmentName string, campaignOnly bool) (string, error) {
	effectiveTargetDB := strings.TrimSpace(targetDB)
	if effectiveTargetDB == "" {
		effectiveTargetDB = strings.TrimSpace(sourceDB)
	}

	if !campaignOnly &&
		effectiveTargetDB == strings.TrimSpace(sourceDB) &&
		strings.TrimSpace(targetAssessmentName) == strings.TrimSpace(assessmentName) {
		return "", ErrCloneOntoItself
	}

	return effectiveTargetDB, nil
}

// Create a clone subcommand
var cloneCmd = &cobra.Command{
	Use:   "clone",
	Short: "Clone an assessment within a single VECTR instance",
	Long: `Clone an assessment within a single VECTR instance.

clone is syntactic sugar for a transfer whose source and target are the same VECTR
instance and the same credentials. Because a clone is by definition a COPY, it always
mints a fresh globalId for the new assessment - that is inherent to the command and
there is no flag to turn it off. If you want to keep the original globalId you are not
cloning: use "vat transfer", which exposes --reset-id as an opt-in.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Set up a context with signal handling
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), vat.VERSION, vat.VatContextValue(version)))
		defer cancel()

		// Handle Ctrl-C (SIGINT) and other termination signals
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			defer signal.Reset()
			<-signalChan
			slog.Info("\nReceived interrupt signal, shutting down gracefully. Ctrl+C again to force shutdown...")
			cancel()
		}()

		// Normalize the user-supplied names once, at the edge, so the read side and
		// the write side can never disagree about which db/assessment is meant.
		cloneSourceDB = strings.TrimSpace(cloneSourceDB)
		cloneTargetDB = strings.TrimSpace(cloneTargetDB)
		cloneAssessmentName = strings.TrimSpace(cloneAssessmentName)
		cloneTargetAssessmentName = strings.TrimSpace(cloneTargetAssessmentName)
		cloneSourceCampaignName = strings.TrimSpace(cloneSourceCampaignName)

		// Resolve the target db and reject a clone onto itself before touching the network
		effectiveTargetDB, err := resolveCloneTarget(cloneSourceDB, cloneTargetDB, cloneAssessmentName, cloneTargetAssessmentName, cloneSourceCampaignName != "")
		if err != nil {
			slog.ErrorContext(ctx, "cannot clone an assessment onto itself, pick a different --target-assessment-name or a different target db (--target-db/--target-env)",
				"db", cloneSourceDB,
				"assessment-name", cloneAssessmentName,
				"target-assessment-name", cloneTargetAssessmentName,
				"error", err)
			os.Exit(1)
		}

		// Read credentials from the file
		credentials, err := os.ReadFile(cloneCredentialsFile)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to read VECTR credentials file", "error", err)
			os.Exit(1)
		}

		// Set up the VECTR client
		client, vectrRestApiCaller, err := util.SetupVectrClient(cloneHostname, strings.TrimSpace(string(credentials)), tlsParams)
		if err != nil {
			slog.ErrorContext(ctx, "could not set up connection to vectr", "hostname", cloneHostname, "error", err)
			os.Exit(1)
		}

		// get the VECTR version (side effect - check the creds as well)
		vectrVersion, err := vectrRestApiCaller.GetVersion(ctx)
		if err != nil {
			if err == util.ErrInvalidAuth {
				slog.ErrorContext(ctx, "could not validate creds", "hostname", cloneHostname, "error", err)
				os.Exit(1)
			}
			slog.ErrorContext(ctx, "could not get vectr version", "hostname", cloneHostname, "error", err)
			os.Exit(1)
		}
		slog.InfoContext(ctx, "validated credentials and fetched vectr version", "hostname", cloneHostname, "vectr-version", vectrVersion)
		enforceVectrVersionCheck(ctx, vectrVersion, cloneHostname)
		versionContext := context.WithValue(ctx, vat.VECTR_VERSION, vat.VatContextValue(vectrVersion))

		// Fetch the assessment data to clone
		slog.InfoContext(versionContext, "Fetching assessment data to clone", "hostname", cloneHostname, "db", cloneSourceDB, "assessment-name", cloneAssessmentName)
		assessmentData, err := vat.SaveAssessmentData(versionContext, client, cloneSourceDB, cloneAssessmentName)
		if err != nil {
			slog.ErrorContext(versionContext, "Failed to fetch the assessment data to clone", "db", cloneSourceDB, "assessment-name", cloneAssessmentName, "error", err)
			os.Exit(1)
		}

		if cloneSourceCampaignName == "" {
			// A clone is a copy, so it always gets a fresh globalId - this is not a user choice.
			optionalParams := &vat.RestoreOptionalParams{
				AssessmentName:             cloneTargetAssessmentName,
				OverrideAssessmentTemplate: cloneOverrideTemplate,
				DeleteOnFailure:            cloneDeleteOnFailure,
				ForceEnvOnly:               cloneForceEnvOnly,
				ResetGlobalId:              true,
			}
			slog.InfoContext(versionContext, "Cloning assessment", "hostname", cloneHostname, "db", effectiveTargetDB, "target-assessment-name", cloneTargetAssessmentName)
			if err := vat.RestoreAssessment(versionContext, client, effectiveTargetDB, assessmentData, optionalParams); err != nil {
				switch {
				case errors.Is(err, vat.ErrAssessmentAlreadyExists):
					slog.ErrorContext(versionContext, "Failed to clone assessment: an assessment with that name already exists, pick a different --target-assessment-name",
						"db", effectiveTargetDB, "target-assessment-name", cloneTargetAssessmentName, "error", err)
				case errors.Is(err, vat.ErrDuplicateGlobalId):
					// Deliberately not logging err: the sentinel's own text advises
					// "retry with --reset-id", a flag clone does not have. The only
					// facts its wrapper adds are the assessment name and the globalId,
					// both of which we have here (RestoreAssessment writes the minted
					// id back onto assessmentData), and the underlying GraphQL detail
					// is already logged by the restore path.
					slog.ErrorContext(versionContext, "Failed to clone assessment: the freshly minted globalId is already present in the target, which should not be reachable since every clone mints a new one - please report this",
						"db", effectiveTargetDB, "target-assessment-name", cloneTargetAssessmentName, "global-id", assessmentData.Assessment.GlobalId)
				default:
					slog.ErrorContext(versionContext, "Failed to clone assessment", "db", effectiveTargetDB, "target-assessment-name", cloneTargetAssessmentName, "error", err)
				}
				os.Exit(1)
			}
		} else {
			// Campaign-only clone into an existing target assessment
			optionalParams := &vat.RestoreOptionalParams{
				ForceEnvOnly: cloneForceEnvOnly,
			}
			slog.InfoContext(versionContext, "Cloning campaign into target assessment", "source-campaign", cloneSourceCampaignName, "db", effectiveTargetDB, "target-assessment", cloneTargetAssessmentName)
			if err := vat.RestoreCampaign(versionContext, client, effectiveTargetDB, assessmentData, cloneSourceCampaignName, cloneTargetAssessmentName, optionalParams); err != nil {
				slog.ErrorContext(versionContext, "Failed to clone campaign into target assessment", "source-campaign", cloneSourceCampaignName, "target-assessment", cloneTargetAssessmentName, "error", err)
				os.Exit(1)
			}
		}

		slog.InfoContext(ctx, "Assessment cloned successfully", "target-assessment-name", cloneTargetAssessmentName, "db", effectiveTargetDB)
	},
}

func init() {
	// Add flags to the clone command
	cloneCmd.Flags().StringVar(&cloneHostname, "hostname", "", "Hostname of the VECTR instance (required)")
	cloneCmd.Flags().StringVar(&cloneCredentialsFile, "vectr-creds-file", "", "Path to the VECTR credentials file (required)")
	cloneCmd.Flags().StringVar(&cloneSourceDB, "db", "", "Database to clone the assessment from (required)")
	cloneCmd.Flags().StringVar(&cloneSourceDB, "env", "", "Alias for --db")
	cloneCmd.Flags().StringVar(&cloneTargetDB, "target-db", "", "Database to clone the assessment into. Defaults to --db")
	cloneCmd.Flags().StringVar(&cloneTargetDB, "target-env", "", "Alias for --target-db")
	cloneCmd.Flags().StringVar(&cloneAssessmentName, "assessment-name", "", "Name of the assessment to clone (required)")
	cloneCmd.Flags().StringVar(&cloneTargetAssessmentName, "target-assessment-name", "", "The assessment name to give the clone (required). The clone always gets a new globalId; use the transfer command if you need to keep the original one.")
	cloneCmd.Flags().BoolVar(&cloneOverrideTemplate, "override-template-assessment", false, "Ignore the template name in the serialized data and load template test cases anyway")
	cloneCmd.Flags().BoolVar(&cloneDeleteOnFailure, "delete-on-failure", false, "In the case of a failure, delete the created assessment from VECTR (does not delete template information). Does not affect single campaign inserts.")
	cloneCmd.Flags().StringVar(&cloneSourceCampaignName, "source-campaign-name", "", "Name of a specific campaign to clone. If set, --target-assessment-name must be an existing assessment.")
	cloneCmd.Flags().BoolVar(&cloneForceEnvOnly, "force-env-only", false, "Ignore any templates associated with test cases, import them in the env only (DANGEROUS)")

	// Mark flags as required
	cloneCmd.MarkFlagRequired("hostname")
	cloneCmd.MarkFlagRequired("vectr-creds-file")
	cloneCmd.MarkFlagsOneRequired("db", "env")
	cloneCmd.MarkFlagRequired("assessment-name")
	cloneCmd.MarkFlagRequired("target-assessment-name")

	// --db/--env and --target-db/--target-env are aliases backed by the same
	// variable, so passing both with different values would silently let parse
	// order decide the database. Exactly one of each pair.
	cloneCmd.MarkFlagsMutuallyExclusive("db", "env")
	cloneCmd.MarkFlagsMutuallyExclusive("target-db", "target-env")
}

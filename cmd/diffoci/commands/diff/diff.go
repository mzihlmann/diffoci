package diff

import (
	"errors"
	"fmt"
	"os"

	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/reproducible-containers/diffoci/cmd/diffoci/backend/backendmanager"
	"github.com/reproducible-containers/diffoci/cmd/diffoci/flagutil"
	"github.com/reproducible-containers/diffoci/cmd/diffoci/imagegetter"
	"github.com/reproducible-containers/diffoci/pkg/diff"
	"github.com/reproducible-containers/diffoci/pkg/localpathutil"
	"github.com/reproducible-containers/diffoci/pkg/platformutil"
	"github.com/spf13/cobra"
)

const Example = `  # Basic
  diffoci diff --semantic alpine:3.18.2 alpine:3.18.3

  # Dump conflicting files to ~/diff
  diffoci diff --semantic --report-dir=~/diff alpine:3.18.2 alpine:3.18.3

  # Compare local Docker images
  diffoci diff --semantic docker://foo docker://bar
`

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "diff IMAGE0 IMAGE1",
		Short:   "Diff images",
		Example: Example,
		Args:    cobra.ExactArgs(2),

		PreRunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			if semantic, _ := cmd.Flags().GetBool("semantic"); semantic {
				flagNames := []string{
					"ignore-history",
					"ignore-file-order",
					"ignore-file-mode-redundant-bits",
					"ignore-file-mtime",
					"ignore-file-atime",
					"ignore-file-ctime",
					"ignore-image-timestamps",
					"ignore-image-name",
					"ignore-tar-format",
					"treat-canonical-paths-equal",
				}
				for _, f := range flagNames {
					if err := flags.Set(f, "true"); err != nil {
						return err
					}
				}
			}
			if ignoreTimestamps, _ := cmd.Flags().GetBool("ignore-timestamps"); ignoreTimestamps {
				flagNames := []string{
					"ignore-file-mtime",
					"ignore-file-atime",
					"ignore-file-ctime",
					"ignore-image-timestamps",
				}
				for _, f := range flagNames {
					if err := flags.Set(f, "true"); err != nil {
						return err
					}
				}
			}
			if ignoreTimestamps, _ := cmd.Flags().GetBool("ignore-file-timestamps"); ignoreTimestamps {
				flagNames := []string{
					"ignore-file-mtime",
					"ignore-file-atime",
					"ignore-file-ctime",
				}
				for _, f := range flagNames {
					if err := flags.Set(f, "true"); err != nil {
						return err
					}
				}
			}
			return nil
		},
		RunE: action,

		DisableFlagsInUseLine: true,
	}

	flags := cmd.Flags()
	flagutil.AddPlatformFlags(flags)
	flags.Bool("ignore-timestamps", false, "Ignore timestamps - Alias for --ignore-*-timestamps=true")
	flags.Bool("ignore-history", false, "Ignore history")
	flags.Bool("ignore-file-order", false, "Ignore file order in tar layers")
	flags.Bool("ignore-file-mode-redundant-bits", false, "Ignore redundant bits of file mode")
	flags.Bool("ignore-file-timestamps", false, "Ignore timestamps on files - Alias for --ignore-file-*time=true")
	flags.Bool("ignore-file-mtime", false, "Ignore mtime timestamps on files")
	flags.Bool("ignore-file-atime", false, "Ignore atime timestamps on files")
	flags.Bool("ignore-file-ctime", false, "Ignore ctime timestamps on files")
	flags.Bool("extra-ignore-file-permissions", false, "Ignore permissions on files")
	flags.Bool("extra-ignore-file-mode", false, "Ignore file mode")
	flags.Bool("extra-ignore-file-content", false, "Ignore the contents of files and compare their size only")
	flags.Bool("extra-ignore-layer-length-mismatch", false, "Ignore if different number of files are touched in the layers")
	flags.StringSlice("extra-ignore-files", []string{}, "Ignore all diffs on specific files")
	flags.Bool("ignore-image-timestamps", false, "Ignore timestamps in image metadata")
	flags.Bool("ignore-image-name", false, "Ignore image name annotation")
	flags.Bool("ignore-tar-format", false, "Ignore tar format")
	flags.Bool("treat-canonical-paths-equal", false, "Treat leading `./` `/` `` in file paths as canonical")
	flags.Bool("semantic", false, "[Recommended] Alias for --ignore-*=true --treat-canonical-paths-equal")

	flags.Bool("verbose", false, "Verbose output")
	flags.String("report-file", "", "Create a report file to the specified path (EXPERIMENTAL)")
	flags.String("report-dir", "", "Create a detailed report in the specified directory")
	flags.String("pull", imagegetter.PullMissing, "Pull mode (always|missing|never)")
	flags.Float64("max-scale", 1.0, "Scale factor for maximum values (e.g., maxTarBlobSize = 4GiB)")
	return cmd
}

func action(cmd *cobra.Command, args []string) error {
	backend, err := backendmanager.NewBackend(cmd)
	if err != nil {
		return err
	}
	ctx := backend.Context(cmd.Context())
	flags := cmd.Flags()
	plats, err := flagutil.ParsePlatformFlags(flags)
	if err != nil {
		return err
	}
	log.G(ctx).Infof("Target platforms: %v", platformutil.FormatSlice(plats))
	platMC := platforms.Any(plats...)

	var options diff.Options
	options.IgnoreHistory, err = flags.GetBool("ignore-history")
	if err != nil {
		return err
	}
	options.IgnoreFileOrder, err = flags.GetBool("ignore-file-order")
	if err != nil {
		return err
	}
	options.IgnoreFileModeRedundantBits, err = flags.GetBool("ignore-file-mode-redundant-bits")
	if err != nil {
		return err
	}
	options.IgnoreFileMTime, err = flags.GetBool("ignore-file-mtime")
	if err != nil {
		return err
	}
	options.IgnoreFileATime, err = flags.GetBool("ignore-file-atime")
	if err != nil {
		return err
	}
	options.IgnoreFileCTime, err = flags.GetBool("ignore-file-ctime")
	if err != nil {
		return err
	}
	options.IgnoreFilePermissions, err = flags.GetBool("extra-ignore-file-permissions")
	if err != nil {
		return err
	}
	options.IgnoreFileMode, err = flags.GetBool("extra-ignore-file-mode")
	if err != nil {
		return err
	}
	options.IgnoreFileContent, err = flags.GetBool("extra-ignore-file-content")
	if err != nil {
		return err
	}
	options.IgnoreLayerLengthMismatch, err = flags.GetBool("extra-ignore-layer-length-mismatch")
	if err != nil {
		return err
	}
	options.IgnoreFiles, err = flags.GetStringSlice("extra-ignore-files")
	if err != nil {
		return err
	}
	options.IgnoreImageTimestamps, err = flags.GetBool("ignore-image-timestamps")
	if err != nil {
		return err
	}
	options.IgnoreImageName, err = flags.GetBool("ignore-image-name")
	if err != nil {
		return err
	}
	options.IgnoreTarFormat, err = flags.GetBool("ignore-tar-format")
	if err != nil {
		return err
	}
	options.CanonicalPaths, err = flags.GetBool("treat-canonical-paths-equal")
	if err != nil {
		return err
	}
	options.ReportFile, err = flags.GetString("report-file")
	if err != nil {
		return err
	}
	if options.ReportFile != "" {
		log.G(ctx).Warn("report-file is experimental. The file format is subject to change.")
		options.ReportFile, err = localpathutil.Expand(options.ReportFile)
		if err != nil {
			return fmt.Errorf("invalid report-file path %q: %w", options.ReportFile, err)
		}
	}
	options.ReportDir, err = flags.GetString("report-dir")
	if err != nil {
		return err
	}
	if options.ReportDir != "" {
		options.ReportDir, err = localpathutil.Expand(options.ReportDir)
		if err != nil {
			return fmt.Errorf("invalid report-dir path %q: %w", options.ReportDir, err)
		}
	}

	options.EventHandler = diff.DefaultEventHandler
	verbose, err := flags.GetBool("verbose")
	if err != nil {
		return err
	}
	if verbose {
		options.EventHandler = diff.VerboseEventHandler
	}

	options.MaxScale, err = flags.GetFloat64("max-scale")
	if err != nil {
		return err
	}

	pullMode, err := flags.GetString("pull")
	if err != nil {
		return err
	}

	ig, err := imagegetter.New(cmd.ErrOrStderr(), backend)
	if err != nil {
		return err
	}

	var imageDescs [2]ocispec.Descriptor
	for i := 0; i < 2; i++ {
		img, err := ig.Get(ctx, args[i], plats, imagegetter.PullMode(pullMode))
		if err != nil {
			return err
		}
		log.G(ctx).Debugf("Input %d: Image %q (%s)", i, img.Name, img.Target.Digest)
		imageDescs[i] = img.Target
	}

	contentStore := backend.ContentStore()

	var exitCode int
	report, err := diff.Diff(ctx, contentStore, imageDescs, platMC, &options)
	if report != nil && len(report.Children) > 0 {
		exitCode = 1
	}
	if err != nil {
		if errors.Is(err, errdefs.ErrUnavailable) {
			err = fmt.Errorf("%w (Hint: specify `--platform` explicitly, e.g., `--platform=linux/amd64`)", err)
		}
		log.G(ctx).Error(err)
		exitCode = 2
	}
	if exitCode != 0 {
		log.G(ctx).Debugf("exiting with code %d", exitCode)
	}
	os.Exit(exitCode)
	/* NOTREACHED */
	return nil
}

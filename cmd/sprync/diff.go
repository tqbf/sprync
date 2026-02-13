package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/tqbf/sprync/pkg/pack"
)

func diffCmd() *cli.Command {
	return &cli.Command{
		Name:      "diff",
		Usage:     "show what push or pull would do",
		ArgsUsage: "<localDir> <sprite>:<remoteDir>",
		Flags: append(syncFlags(),
			&cli.StringFlag{
				Name:  "mode",
				Value: "push",
				Usage: "diff direction: push or pull",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "JSON output",
			},
		),
		Action: diffAction,
	}
}

type diffJSON struct {
	Transfers []diffTransfer `json:"transfers"`
	Deletes   []string       `json:"deletes"`
	Summary   diffSummary    `json:"summary"`
}

type diffTransfer struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Reason string `json:"reason"`
}

type diffSummary struct {
	TransferCount int   `json:"transfer_count"`
	TransferBytes int64 `json:"transfer_bytes"`
	DeleteCount   int   `json:"delete_count"`
}

func diffAction(c *cli.Context) error {
	if c.NArg() != 2 {
		return fmt.Errorf(
			"usage: sprync diff <localDir> <sprite>:<remoteDir>",
		)
	}
	localDir := c.Args().Get(0)
	sprite, remoteDir, err := parseTarget(c.Args().Get(1))
	if err != nil {
		return err
	}

	token, err := requireToken(c, sprite)
	if err != nil {
		return err
	}

	mode := c.String("mode")
	if mode != "push" && mode != "pull" {
		return fmt.Errorf("--mode must be push or pull")
	}

	ctx, cancel := contextWithTimeout(c)
	defer cancel()

	client := newClient(c, token)
	excludes := c.StringSlice("exclude")
	deleteOn := c.Bool("delete")

	sess, err := openSession(ctx, client, sprite)
	if err != nil {
		return err
	}
	defer sess.Close(ctx)

	entries, exists, elapsed, err := sess.Manifest(
		remoteDir, excludes,
	)
	if err != nil {
		return fmt.Errorf("remote manifest: %w", err)
	}
	slog.Debug("remote manifest",
		"count", len(entries),
		"exists", exists,
		"elapsed", elapsed,
	)

	remoteM := entriesToManifest(entries)

	localM, err := pack.WalkLocal(localDir, excludes)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("walk local: %w", err)
	}
	if localM == nil {
		localM = make(pack.Manifest)
	}

	var sourceM, targetM pack.Manifest
	if mode == "push" {
		sourceM, targetM = localM, remoteM
	} else {
		if !exists {
			return fmt.Errorf(
				"remote directory does not exist",
			)
		}
		sourceM, targetM = remoteM, localM
	}

	diff := pack.ComputeDiff(sourceM, targetM, deleteOn)

	if c.Bool("json") {
		return printDiffJSON(
			diff, sourceM, targetM,
		)
	}

	if len(diff.Uploads) == 0 && len(diff.Deletes) == 0 {
		fmt.Println("Already in sync.")
		return nil
	}

	printChanges(
		diff.Uploads, diff.Deletes, sourceM, targetM,
	)

	var b strings.Builder
	size := transferSize(diff.Uploads, sourceM)
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b,
		"%d to transfer (%s)",
		len(diff.Uploads), humanBytes(size),
	)
	if len(diff.Deletes) > 0 {
		fmt.Fprintf(&b,
			", %d to delete", len(diff.Deletes),
		)
	}
	fmt.Fprintf(&b, "\n")
	fmt.Print(b.String())
	return nil
}

func printDiffJSON(
	diff pack.DiffResult,
	sourceM, targetM pack.Manifest,
) error {
	out := diffJSON{
		Transfers: make([]diffTransfer, 0, len(diff.Uploads)),
		Deletes:   diff.Deletes,
		Summary: diffSummary{
			TransferCount: len(diff.Uploads),
			DeleteCount:   len(diff.Deletes),
		},
	}
	if out.Deletes == nil {
		out.Deletes = []string{}
	}

	for _, p := range diff.Uploads {
		reason := "new"
		if _, ok := targetM[p]; ok {
			reason = "changed"
		}
		size := int64(0)
		if e, ok := sourceM[p]; ok {
			size = e.Size
		}
		out.Transfers = append(out.Transfers, diffTransfer{
			Path:   p,
			Size:   size,
			Reason: reason,
		})
		out.Summary.TransferBytes += size
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/tqbf/sprync/pkg/pack"
)

func pullCmd() *cli.Command {
	return &cli.Command{
		Name:      "pull",
		Usage:     "pull sprite directory to local",
		ArgsUsage: "<sprite>:<remoteDir> <localDir>",
		Flags:     syncFlags(),
		Action:    pullAction,
	}
}

func pullAction(c *cli.Context) error {
	if c.NArg() != 2 {
		return fmt.Errorf(
			"usage: sprync pull <sprite>:<remoteDir> <localDir>",
		)
	}
	sprite, remoteDir, err := parseTarget(c.Args().Get(0))
	if err != nil {
		return err
	}
	localDir := c.Args().Get(1)

	token, err := requireToken(c, sprite)
	if err != nil {
		return err
	}

	ctx, cancel := contextWithTimeout(c)
	defer cancel()

	client := newClient(c, token)
	excludes := c.StringSlice("exclude")
	compress := c.Bool("compress")
	deleteOn := c.Bool("delete")
	dryRun := c.Bool("dry-run")

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

	if !exists {
		return fmt.Errorf(
			"remote directory %s does not exist", remoteDir,
		)
	}

	remoteM := entriesToManifest(entries)

	localM, err := pack.WalkLocal(localDir, excludes)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("walk local: %w", err)
	}
	if localM == nil {
		localM = make(pack.Manifest)
	}
	slog.Debug("local manifest",
		"count", len(localM),
	)

	diff := pack.ComputeDiff(remoteM, localM, deleteOn)
	downloads := diff.Uploads
	deletes := diff.Deletes

	if len(downloads) == 0 && len(deletes) == 0 {
		fmt.Println("Already in sync.")
		return nil
	}

	fmt.Printf(
		"Pulling from %s:%s\n", sprite, remoteDir,
	)
	printChanges(downloads, deletes, remoteM, localM)

	var b strings.Builder
	size := transferSize(downloads, remoteM)
	fmt.Fprintf(&b,
		"%d to transfer (%s)",
		len(downloads), humanBytes(size),
	)
	if len(deletes) > 0 {
		fmt.Fprintf(&b, ", %d to delete", len(deletes))
	}
	fmt.Fprintf(&b, "\n")
	fmt.Print(b.String())

	if dryRun {
		return nil
	}

	if len(downloads) > 0 {
		packResult, err := sess.Pack(
			remoteDir, downloads, compress,
		)
		if err != nil {
			return fmt.Errorf("remote pack: %w", err)
		}
		slog.Debug("packed",
			"dest", packResult.Dest,
			"size", packResult.Size,
			"count", packResult.Count,
		)

		body, err := client.FSRead(
			ctx, sprite, packResult.Dest,
		)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
		defer body.Close()

		count, err := pack.UnpackTar(
			body, localDir, compress,
		)
		if err != nil {
			return fmt.Errorf("unpack: %w", err)
		}
		fmt.Printf(
			"Transferred %d files (%s)\n",
			count, humanBytes(size),
		)
	}

	if len(deletes) > 0 {
		deleted := 0
		for _, p := range deletes {
			target := filepath.Join(localDir, p)
			if err := os.RemoveAll(target); err != nil {
				slog.Warn("delete failed",
					"path", p, "err", err,
				)
				continue
			}
			deleted++
		}
		fmt.Printf("Deleted %d files\n", deleted)
	}

	return nil
}

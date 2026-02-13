package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/tqbf/sprync/pkg/pack"
)

func pushCmd() *cli.Command {
	return &cli.Command{
		Name:      "push",
		Usage:     "push local directory to sprite",
		ArgsUsage: "<localDir> <sprite>:<remoteDir>",
		Flags:     syncFlags(),
		Action:    pushAction,
	}
}

func pushAction(c *cli.Context) error {
	if c.NArg() != 2 {
		return fmt.Errorf(
			"usage: sprync push <localDir> <sprite>:<remoteDir>",
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

	ctx, cancel := contextWithTimeout(c)
	defer cancel()

	var (
		client   = newClient(c, token)
		excludes = c.StringSlice("exclude")
		compress = c.Bool("compress")
		deleteOn = c.Bool("delete")
		dryRun   = c.Bool("dry-run")
	)

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
	if err != nil {
		return fmt.Errorf("walk local: %w", err)
	}
	slog.Debug("local manifest",
		"count", len(localM),
	)

	var uploads, deletes []string
	if !exists {
		for p := range localM {
			uploads = append(uploads, p)
		}
		sort.Strings(uploads)
	} else {
		diff := pack.ComputeDiff(
			localM, remoteM, deleteOn,
		)
		uploads = diff.Uploads
		deletes = diff.Deletes
	}

	if len(uploads) == 0 && len(deletes) == 0 {
		fmt.Println("Already in sync.")
		return nil
	}

	tag := ""
	if !exists {
		tag = " (new)"
	}
	fmt.Printf(
		"Pushing to %s:%s%s\n", sprite, remoteDir, tag,
	)
	printChanges(uploads, deletes, localM, remoteM)

	var b strings.Builder
	size := transferSize(uploads, localM)
	fmt.Fprintf(&b,
		"%d to transfer (%s)",
		len(uploads), humanBytes(size),
	)
	if len(deletes) > 0 {
		fmt.Fprintf(&b, ", %d to delete", len(deletes))
	}
	fmt.Fprintf(&b, "\n")
	fmt.Print(b.String())

	if dryRun {
		return nil
	}

	if len(uploads) > 0 {
		var buf bytes.Buffer
		_, err := pack.PackTar(
			localDir, uploads, &buf, compress,
		)
		if err != nil {
			return fmt.Errorf("pack: %w", err)
		}

		dest := remoteTmpPath(compress)
		err = client.FSWrite(
			ctx, sprite, dest, "", false, &buf,
		)
		if err != nil {
			return fmt.Errorf("upload: %w", err)
		}

		result, err := sess.Extract(
			remoteDir, dest, compress,
		)
		if err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		fmt.Printf(
			"Transferred %d files (%s)\n",
			result.Count, humanBytes(size),
		)
	}

	if len(deletes) > 0 {
		result, err := sess.Delete(remoteDir, deletes)
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		fmt.Printf("Deleted %d files\n", result.Count)
	}

	return nil
}

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
		Name:  "push",
		Usage: "push directory to sprite",
		ArgsUsage: "<localDir|sprite:dir>" +
			" <sprite:dir>",
		Flags:  syncFlags(),
		Action: pushAction,
	}
}

func pushAction(c *cli.Context) error {
	if c.NArg() != 2 {
		return fmt.Errorf(
			"usage: sprync push <src> <sprite:dir>",
		)
	}

	srcSprite, srcDir, srcErr := parseTarget(
		c.Args().Get(0),
	)
	dstSprite, dstDir, err := parseTarget(
		c.Args().Get(1),
	)
	if err != nil {
		return err
	}

	if srcErr == nil {
		return spriteToSpritePush(c,
			srcSprite, srcDir,
			dstSprite, dstDir,
		)
	}
	return localToSpritePush(c,
		c.Args().Get(0), dstSprite, dstDir,
	)
}

func localToSpritePush(
	c *cli.Context,
	localDir, sprite, remoteDir string,
) error {
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

func spriteToSpritePush(
	c *cli.Context,
	srcSprite, srcDir, dstSprite, dstDir string,
) error {
	token, err := requireToken(c, srcSprite)
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

	srcSess, err := openSession(ctx, client, srcSprite)
	if err != nil {
		return fmt.Errorf("src session: %w", err)
	}
	defer srcSess.Close(ctx)

	dstSess, err := openSession(ctx, client, dstSprite)
	if err != nil {
		return fmt.Errorf("dst session: %w", err)
	}
	defer dstSess.Close(ctx)

	srcEntries, srcExists, _, err := srcSess.Manifest(
		srcDir, excludes,
	)
	if err != nil {
		return fmt.Errorf("src manifest: %w", err)
	}
	if !srcExists {
		return fmt.Errorf(
			"source %s:%s does not exist",
			srcSprite, srcDir,
		)
	}
	srcM := entriesToManifest(srcEntries)

	dstEntries, dstExists, _, err := dstSess.Manifest(
		dstDir, excludes,
	)
	if err != nil {
		return fmt.Errorf("dst manifest: %w", err)
	}
	dstM := entriesToManifest(dstEntries)

	var uploads, deletes []string
	if !dstExists {
		for p := range srcM {
			uploads = append(uploads, p)
		}
		sort.Strings(uploads)
	} else {
		diff := pack.ComputeDiff(
			srcM, dstM, deleteOn,
		)
		uploads = diff.Uploads
		deletes = diff.Deletes
	}

	if len(uploads) == 0 && len(deletes) == 0 {
		fmt.Println("Already in sync.")
		return nil
	}

	tag := ""
	if !dstExists {
		tag = " (new)"
	}
	fmt.Printf(
		"Pushing %s:%s -> %s:%s%s\n",
		srcSprite, srcDir,
		dstSprite, dstDir, tag,
	)
	printChanges(uploads, deletes, srcM, dstM)

	var b strings.Builder
	size := transferSize(uploads, srcM)
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
		dest := remoteTmpPath(compress)
		destURL := client.FSWriteURL(
			dstSprite, dest, "", false,
		)

		result, err := srcSess.Transfer(
			srcDir, uploads, compress,
			destURL, token,
		)
		if err != nil {
			return fmt.Errorf("transfer: %w", err)
		}

		_, err = dstSess.Extract(
			dstDir, dest, compress,
		)
		if err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		fmt.Printf(
			"Transferred %d files (%s)\n",
			result.Count, humanBytes(result.Size),
		)
	}

	if len(deletes) > 0 {
		result, err := dstSess.Delete(dstDir, deletes)
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		fmt.Printf("Deleted %d files\n", result.Count)
	}

	return nil
}

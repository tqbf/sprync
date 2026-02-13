package main

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/tqbf/sprync/pkg/embedded"
	"github.com/tqbf/sprync/pkg/protocol"
)

func doctorCmd() *cli.Command {
	return &cli.Command{
		Name:      "doctor",
		Usage:     "verify sprite connectivity",
		ArgsUsage: "<sprite>",
		Action:    doctorAction,
	}
}

func doctorAction(c *cli.Context) error {
	if c.NArg() != 1 {
		return fmt.Errorf("usage: sprync doctor <sprite>")
	}
	sprite := c.Args().Get(0)

	token, err := requireToken(c, sprite)
	if err != nil {
		return err
	}

	ctx, cancel := contextWithTimeout(c)
	defer cancel()

	client := newClient(c, token)

	fmt.Printf("Sprite: %s\n", sprite)

	info, err := client.GetSprite(ctx, sprite)
	if err != nil {
		fmt.Printf("  API: FAIL (%v)\n", err)
		return fmt.Errorf("sprite check failed")
	}
	fmt.Printf("  Status: %s\n", info.Status)
	fmt.Printf("  API: ok\n")

	t := time.Now()
	sess, err := protocol.OpenSession(
		ctx, client, sprite, embedded.SpryncdBinary,
	)
	if err != nil {
		fmt.Printf("  Session: FAIL (%v)\n", err)
		return fmt.Errorf("session check failed")
	}
	defer sess.Close(ctx)
	connectMs := time.Since(t).Milliseconds()

	fmt.Printf(
		"  Upload: ok (spryncd %s, %s)\n",
		sess.Version,
		humanBytes(int64(len(embedded.SpryncdBinary))),
	)
	fmt.Printf(
		"  Exec: ok (ready in %dms)\n", connectMs,
	)

	entries, _, manifestElapsed, err := sess.Manifest(
		"/tmp", nil,
	)
	if err != nil {
		fmt.Printf("  Manifest: FAIL (%v)\n", err)
		return fmt.Errorf("manifest check failed")
	}
	fmt.Printf(
		"  Manifest: ok (%d entries in /tmp, %dms)\n",
		len(entries),
		manifestElapsed.Milliseconds(),
	)

	fmt.Println("\nAll checks passed.")
	return nil
}

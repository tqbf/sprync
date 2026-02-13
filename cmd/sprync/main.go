package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/tqbf/sprync/pkg/embedded"
	"github.com/tqbf/sprync/pkg/pack"
	"github.com/tqbf/sprync/pkg/protocol"
	"github.com/tqbf/sprync/pkg/spriteapi"
	"github.com/tqbf/sprync/pkg/spriteauth"
)

const appVersion = "0.1.0"

func main() {
	app := &cli.App{
		Name:  "sprync",
		Usage: "sync directories with Sprite VMs",
		Before: func(c *cli.Context) error {
			configureLogging(c.Bool("verbose"))
			return nil
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "token",
				EnvVars: []string{"SPRITE_TOKEN"},
				Usage:   "Sprite API token",
			},
			&cli.StringFlag{
				Name:  "api",
				Value: "https://api.sprites.dev",
				Usage: "Sprite API base URL",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Value: 5 * time.Minute,
				Usage: "operation timeout",
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "verbose output",
			},
		},
		Commands: []*cli.Command{
			pushCmd(),
			pullCmd(),
			diffCmd(),
			doctorCmd(),
			{
				Name:  "version",
				Usage: "print version",
				Action: func(c *cli.Context) error {
					fmt.Println(appVersion)
					return nil
				},
			},
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func syncFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:  "delete",
			Usage: "delete extra files on target",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "show what would happen",
		},
		&cli.StringSliceFlag{
			Name:  "exclude",
			Usage: "exclude pattern (repeatable)",
		},
		&cli.BoolFlag{
			Name:  "compress",
			Value: true,
			Usage: "gzip tarballs",
		},
	}
}

func configureLogging(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
		}),
	))
}

func requireToken(
	c *cli.Context, sprite string,
) (string, error) {
	tok := c.String("token")
	if tok != "" {
		return tok, nil
	}
	slog.Debug("no token provided, trying sprite CLI")
	tok, err := spriteauth.ResolveToken(sprite)
	if err != nil {
		return "", fmt.Errorf(
			"no token: set SPRITE_TOKEN, "+
				"use --token, or log in with "+
				"'sprite login': %w",
			err,
		)
	}
	return tok, nil
}

func newClient(
	c *cli.Context, token string,
) *spriteapi.Client {
	api := strings.TrimSuffix(c.String("api"), "/")
	return spriteapi.New(api+"/v1/sprites", token)
}

func parseTarget(s string) (string, string, error) {
	sprite, dir, ok := strings.Cut(s, ":")
	if !ok || sprite == "" || dir == "" {
		return "", "", fmt.Errorf(
			"invalid target %q (want sprite:dir)", s,
		)
	}
	return sprite, dir, nil
}

func entriesToManifest(
	entries []pack.ManifestEntry,
) pack.Manifest {
	m := make(pack.Manifest, len(entries))
	for _, e := range entries {
		m[e.Path] = e
	}
	return m
}

func remoteTmpPath(compress bool) string {
	var b [8]byte
	rand.Read(b[:])
	ext := ".tar"
	if compress {
		ext = ".tar.gz"
	}
	return fmt.Sprintf(
		"/tmp/sprync-%s%s",
		hex.EncodeToString(b[:]),
		ext,
	)
}

func contextWithTimeout(
	c *cli.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.Background(),
		c.Duration("timeout"),
	)
}

func openSession(
	ctx context.Context,
	client *spriteapi.Client,
	sprite string,
) (*protocol.Session, error) {
	sess, err := protocol.OpenSession(
		ctx, client, sprite, embedded.SpryncdBinary,
	)
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	return sess, nil
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf(
			"%.1f MB", float64(n)/(1<<20),
		)
	case n >= 1<<10:
		return fmt.Sprintf(
			"%.1f KB", float64(n)/(1<<10),
		)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func printChanges(
	transfers, deletes []string,
	sourceM, targetM pack.Manifest,
) {
	var b strings.Builder
	for _, p := range transfers {
		prefix := "+"
		if _, ok := targetM[p]; ok {
			prefix = "~"
		}
		if e, ok := sourceM[p]; ok {
			fmt.Fprintf(&b,
				"  %s %s (%s)\n",
				prefix, p, humanBytes(e.Size),
			)
		}
	}
	for _, p := range deletes {
		fmt.Fprintf(&b, "  - %s\n", p)
	}
	fmt.Print(b.String())
}

func transferSize(
	paths []string, m pack.Manifest,
) int64 {
	var total int64
	for _, p := range paths {
		if e, ok := m[p]; ok {
			total += e.Size
		}
	}
	return total
}

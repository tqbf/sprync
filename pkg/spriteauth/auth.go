package spriteauth

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
)

var orgLineRe = regexp.MustCompile(
	`^\s*\d+\.\s+(\S+)`,
)

func ResolveToken(sprite string) (string, error) {
	spriteBin, err := exec.LookPath("sprite")
	if err != nil {
		return "", fmt.Errorf(
			"sprite CLI not found: %w", err,
		)
	}

	orgs, err := listOrgs(spriteBin)
	if err != nil {
		return "", fmt.Errorf("list orgs: %w", err)
	}

	org, err := findOrgForSprite(spriteBin, orgs, sprite)
	if err != nil {
		return "", err
	}

	slog.Debug("resolved sprite org",
		"sprite", sprite, "org", org,
	)

	token, err := getOrgToken(spriteBin, org)
	if err != nil {
		return "", fmt.Errorf(
			"get token for org %s: %w", org, err,
		)
	}

	return token, nil
}

func listOrgs(spriteBin string) ([]string, error) {
	out, err := exec.Command(
		spriteBin, "org", "list",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("sprite org list: %w", err)
	}

	var orgs []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		m := orgLineRe.FindStringSubmatch(scanner.Text())
		if m != nil {
			orgs = append(orgs, m[1])
		}
	}
	if len(orgs) == 0 {
		return nil, fmt.Errorf(
			"no orgs found; run 'sprite login' first",
		)
	}
	return orgs, nil
}

type spritesResponse struct {
	Sprites []struct {
		Name string `json:"name"`
	} `json:"sprites"`
}

func findOrgForSprite(
	spriteBin string,
	orgs []string,
	sprite string,
) (string, error) {
	for _, org := range orgs {
		out, err := exec.Command(
			spriteBin, "api", "-o", org, "/sprites",
		).Output()
		if err != nil {
			slog.Debug("failed to list sprites",
				"org", org, "err", err,
			)
			continue
		}

		var resp spritesResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			slog.Debug("failed to parse sprites JSON",
				"org", org, "err", err,
			)
			continue
		}

		for _, s := range resp.Sprites {
			if s.Name == sprite {
				return org, nil
			}
		}
	}
	return "", fmt.Errorf(
		"sprite %q not found in any org", sprite,
	)
}

func getOrgToken(
	spriteBin string,
	org string,
) (string, error) {
	cmd := exec.Command(
		spriteBin, "api", "-o", org, "/", "-v",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Run()

	scanner := bufio.NewScanner(&stderr)
	for scanner.Scan() {
		line := scanner.Text()
		_, after, ok := strings.Cut(
			line, "authorization: Bearer ",
		)
		if !ok {
			continue
		}
		token := strings.TrimRight(after, "\r")
		if token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf(
		"no Bearer token in sprite api output",
	)
}

//go:build windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// run_windows.go — the real Windows wiring of the friend app. The install /
// update decisions live in flow.go (pure, tested); this file only supplies the
// OS deps. It is compiled only with GOOS=windows; the Linux build uses
// run_other.go so `go build ./...` and the test suite stay green on the box.

// realDeps wires the flow's seams to the real Windows environment.
func realDeps() deps {
	return deps{
		exec: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.CombinedOutput()
		},
		fetch: func(ctx context.Context, url string) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		},
		homeDir: os.UserHomeDir,
		positions: func(ctx context.Context) (float64, error) {
			return 0, fmt.Errorf("flatness check not wired (no friend auth token)")
		},
		writeFile: os.WriteFile,
		mkdirAll:  os.MkdirAll,
		rename:    os.Rename,
		readFile:  os.ReadFile,
		readLine: func(prompt string) (string, error) {
			fmt.Print(prompt)
			r := bufio.NewReader(os.Stdin)
			s, err := r.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", err
			}
			return strings.TrimSpace(s), nil
		},
		allowedSigners: []byte(friendAllowedSigners),
	}
}

// run is the app entry: first run = install; every later start = update check.
func run(args []string) error {
	_ = args
	d := realDeps()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	home, err := d.homeDir()
	if err != nil {
		return err
	}

	// First run: no connect.json yet → ask for the install code.
	if _, err := d.readFile(connectJSONPath(home)); err != nil {
		code, err := d.readLine("Paste your VL install code: ")
		if err != nil {
			return err
		}
		if err := installFirstRun(ctx, d, code); err != nil {
			return err
		}
		fmt.Println("VL installed — open the VL icon on your desktop.")
		return nil
	}

	// Every start: check for updates from the friend's own bot.
	cfg, err := readConnectConfig(d, home)
	if err != nil {
		return err
	}
	st, err := readAppState(d, home)
	if err != nil {
		return err
	}
	base := fmt.Sprintf("http://%s:%d", cfg.Host, st.WebPort)
	dec, err := RunUpdateCheck(ctx, d, base, false)
	if err != nil {
		return err
	}
	if dec.Applied {
		fmt.Println("Updated ✓ — restart NinjaTrader to load the new add-on.")
	} else if dec.AddonUpdateAvailable || dec.AppUpdateAvailable {
		fmt.Println("Update deferred (Later).")
	}
	return nil
}

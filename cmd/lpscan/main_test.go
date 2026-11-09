package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dorman99/plutus/pkg/geckoterminal"
	"github.com/dorman99/plutus/pkg/lpscan"
)

func fixtureSource(t *testing.T) lpscan.PoolSource {
	t.Helper()
	f, err := os.Open("../../pkg/lpscan/testdata/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pools, err := geckoterminal.DecodePools(f)
	if err != nil {
		t.Fatal(err)
	}
	return lpscan.PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		if page > 1 {
			return nil, nil
		}
		return pools, nil
	})
}

func runCLI(t *testing.T, src lpscan.PoolSource, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, &out, &errOut, src)
	return code, out.String(), errOut.String()
}

func TestInvalidFlagsExit2(t *testing.T) {
	tests := map[string][]string{
		"--window must be one of 5m, 1h, 6h, 24h": {"--window", "4h"},
		"--top must be between 1 and 50":          {"--top", "0"},
		"--max-mcap must be > 0":                  {"--max-mcap", "-1"},
		"-min-volume":                             {"--min-volume", "lots"},
		"--timeout must be > 0":                   {"--timeout", "0s"},
		"flag provided but not defined: -foo":     {"--foo"},
	}
	for want, args := range tests {
		code, out, errOut := runCLI(t, nil, args...)
		if code != exitParam || !strings.Contains(errOut, want) || out != "" {
			t.Errorf("%v: code=%d stderr=%q", args, code, errOut)
		}
		if strings.Count(errOut, "\n") != 1 {
			t.Errorf("%v: want a one-line message, got %q", args, errOut)
		}
	}
}

func TestDefaultsPrintTable(t *testing.T) {
	code, out, _ := runCLI(t, fixtureSource(t))
	if code != exitOK {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"window=6h", "min-vol=$25.0K", "★2", "Legend:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
}

func TestJSONOutput(t *testing.T) {
	code, out, _ := runCLI(t, fixtureSource(t), "--json", "--top", "3")
	if code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var r lpscan.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 3 || r.Pick == nil || len(r.Results[0].CoinAddress) != 42 {
		t.Errorf("json result = %+v", r)
	}
}

func TestZeroMatchesExit0(t *testing.T) {
	code, out, _ := runCLI(t, fixtureSource(t), "--min-liquidity", "1000000000")
	if code != exitOK || !strings.Contains(out, "No coins matched.") {
		t.Errorf("code=%d out=%s", code, out)
	}
}

func TestAPIFailureExit1(t *testing.T) {
	src := lpscan.PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		return nil, errors.New("HTTP 503 after 4 attempt(s)")
	})
	code, out, errOut := runCLI(t, src)
	if code != exitAPI || out != "" || !strings.Contains(errOut, "503") {
		t.Errorf("code=%d out=%q stderr=%q", code, out, errOut)
	}
}

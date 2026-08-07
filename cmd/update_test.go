package cmd

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/ryanmccool/static-mangal/constant"
	"github.com/ryanmccool/static-mangal/selfupdate"
)

func TestUpdateCommandCheckOutputAndConfig(t *testing.T) {
	originalCheck := updateCheck
	originalApply := updateApply
	originalOut := updateCmd.OutOrStdout()
	originalContext := updateCmd.Context()
	t.Cleanup(func() {
		updateCheck = originalCheck
		updateApply = originalApply
		updateCmd.SetOut(originalOut)
		updateCmd.SetContext(originalContext)
		_ = updateCmd.Flags().Set("check", "false")
	})

	var output bytes.Buffer
	var gotConfig selfupdate.Config
	updateCmd.SetOut(&output)
	updateCmd.SetContext(context.Background())
	_ = updateCmd.Flags().Set("check", "true")
	updateCheck = func(ctx context.Context, config selfupdate.Config) (selfupdate.Result, error) {
		gotConfig = config
		return selfupdate.Result{CurrentVersion: "0.2.0", LatestVersion: "0.3.0", UpdateAvailable: true}, nil
	}
	updateApply = func(context.Context, selfupdate.Config) (selfupdate.Result, error) {
		t.Fatal("check must not apply an update")
		return selfupdate.Result{}, nil
	}

	if err := updateCmd.RunE(updateCmd, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "Current version: 0.2.0\nUpdate available: 0.3.0\n"; got != want {
		t.Fatalf("check output = %q, want %q", got, want)
	}
	if gotConfig.CurrentVersion != constant.Version || gotConfig.Distribution != constant.Distribution {
		t.Fatalf("update config = %+v", gotConfig)
	}
}

func TestUpdateCommandLocalAheadAndApplyOutput(t *testing.T) {
	originalCheck := updateCheck
	originalApply := updateApply
	originalOut := updateCmd.OutOrStdout()
	originalContext := updateCmd.Context()
	originalDistribution := constant.Distribution
	t.Cleanup(func() {
		updateCheck = originalCheck
		updateApply = originalApply
		updateCmd.SetOut(originalOut)
		updateCmd.SetContext(originalContext)
		_ = updateCmd.Flags().Set("check", "false")
		constant.Distribution = originalDistribution
	})

	var output bytes.Buffer
	updateCmd.SetOut(&output)
	updateCmd.SetContext(context.Background())
	_ = updateCmd.Flags().Set("check", "true")
	updateCheck = func(context.Context, selfupdate.Config) (selfupdate.Result, error) {
		return selfupdate.Result{CurrentVersion: "0.4.0", LatestVersion: "0.3.0"}, selfupdate.ErrLocalAhead
	}
	if err := updateCmd.RunE(updateCmd, nil); !errors.Is(err, selfupdate.ErrLocalAhead) {
		t.Fatalf("local-ahead error = %v", err)
	}
	if !strings.Contains(output.String(), "Refusing downgrade") {
		t.Fatalf("local-ahead output = %q", output.String())
	}

	output.Reset()
	_ = updateCmd.Flags().Set("check", "false")
	constant.Distribution = "archive"
	var gotConfig selfupdate.Config
	updateApply = func(_ context.Context, config selfupdate.Config) (selfupdate.Result, error) {
		gotConfig = config
		return selfupdate.Result{CurrentVersion: "0.2.0", LatestVersion: "0.3.0", UpdateAvailable: true}, nil
	}
	if err := updateCmd.RunE(updateCmd, nil); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "Updating static-mangal from version 0.2.0...\nUpdated static-mangal from 0.2.0 to 0.3.0.\nRestart static-mangal to use the new version.\n" {
		t.Fatalf("apply output = %q", got)
	}
	if gotConfig.CurrentVersion != constant.Version || gotConfig.Distribution != "archive" {
		t.Fatalf("apply config = %+v, want archive metadata", gotConfig)
	}
}

func TestUpdateCommandCommittedWarningOutput(t *testing.T) {
	originalApply := updateApply
	originalOut := updateCmd.OutOrStdout()
	originalContext := updateCmd.Context()
	originalDistribution := constant.Distribution
	t.Cleanup(func() {
		updateApply = originalApply
		updateCmd.SetOut(originalOut)
		updateCmd.SetContext(originalContext)
		constant.Distribution = originalDistribution
		_ = updateCmd.Flags().Set("check", "false")
	})

	var output bytes.Buffer
	updateCmd.SetOut(&output)
	updateCmd.SetContext(context.Background())
	_ = updateCmd.Flags().Set("check", "false")
	constant.Distribution = "archive"
	warning := errors.New("lock cleanup warning")
	updateApply = func(context.Context, selfupdate.Config) (selfupdate.Result, error) {
		return selfupdate.Result{CurrentVersion: "0.2.0", LatestVersion: "0.3.0", UpdateAvailable: true}, &selfupdate.CommittedUpdateError{WarningErr: warning}
	}

	err := updateCmd.RunE(updateCmd, nil)
	if !errors.Is(err, warning) {
		t.Fatalf("expected committed warning error, got %v", err)
	}
	if !strings.Contains(output.String(), "Updated static-mangal from 0.2.0 to 0.3.0") || !strings.Contains(output.String(), "cleanup or durability") {
		t.Fatalf("committed warning output = %q", output.String())
	}
}

func TestUpdateCommandNonArchiveApplyRefusal(t *testing.T) {
	for _, distribution := range []string{"development", "package"} {
		t.Run(distribution, func(t *testing.T) {
			originalApply := updateApply
			originalOut := updateCmd.OutOrStdout()
			originalContext := updateCmd.Context()
			t.Cleanup(func() {
				updateApply = originalApply
				updateCmd.SetOut(originalOut)
				updateCmd.SetContext(originalContext)
				_ = updateCmd.Flags().Set("check", "false")
			})
			var gotConfig selfupdate.Config
			updateApply = func(_ context.Context, config selfupdate.Config) (selfupdate.Result, error) {
				gotConfig = config
				if config.Distribution != "archive" {
					return selfupdate.Result{}, selfupdate.ErrUnsupportedDistribution
				}
				return selfupdate.Result{UpdateAvailable: true}, nil
			}
			err := runUpdateApply(context.Background(), updateCmd, selfupdate.Config{CurrentVersion: constant.Version, Distribution: distribution})
			if !errors.Is(err, selfupdate.ErrUnsupportedDistribution) || gotConfig.Distribution != distribution {
				t.Fatalf("distribution=%s config=%+v err=%v", distribution, gotConfig, err)
			}
			_, err = selfupdate.Apply(context.Background(), selfupdate.Config{CurrentVersion: constant.Version, Distribution: distribution})
			if runtime.GOOS == "windows" {
				if !errors.Is(err, selfupdate.ErrUnsupportedPlatform) {
					t.Fatalf("Windows selfupdate apply distribution=%s error=%v", distribution, err)
				}
			} else if !errors.Is(err, selfupdate.ErrUnsupportedDistribution) {
				t.Fatalf("selfupdate apply distribution=%s error=%v", distribution, err)
			}
		})
	}
}

func TestUpdateCommandRejectsArguments(t *testing.T) {
	if err := updateCmd.Args(updateCmd, []string{"unexpected"}); err == nil {
		t.Fatal("expected cobra.NoArgs refusal")
	}
}

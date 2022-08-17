package command

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestVarInitCommand_Implements(t *testing.T) {
	ci.Parallel(t)
	var _ cli.Command = &VarInitCommand{}
}

func TestVarInitCommand_Run_HCL(t *testing.T) {
	ci.Parallel(t)
	ui := cli.NewMockUi()
	cmd := &VarInitCommand{Meta: Meta{Ui: ui}}

	// Fails on misuse
	code := cmd.Run([]string{"some", "bad", "args"})
	require.Equal(t, 1, code)
	require.Contains(t, ui.ErrorWriter.String(), commandErrorText(cmd))
	ui.ErrorWriter.Reset()

	// Ensure we change the cwd back
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	// Create a temp dir and change into it
	dir := t.TempDir()

	err = os.Chdir(dir)
	require.NoError(t, err)

	// Works if the file doesn't exist
	code = cmd.Run([]string{})
	require.Empty(t, ui.ErrorWriter.String())
	require.Zero(t, code)

	content, err := ioutil.ReadFile(DefaultHclVarInitName)
	require.NoError(t, err)
	require.Equal(t, defaultHclVarSpec, string(content))

	// Fails if the file exists
	code = cmd.Run([]string{})
	require.Contains(t, ui.ErrorWriter.String(), "exists")
	require.Equal(t, 1, code)
	ui.ErrorWriter.Reset()

	// Works if file is passed
	code = cmd.Run([]string{"mytest.hcl"})
	require.Empty(t, ui.ErrorWriter.String())
	require.Zero(t, code)

	content, err = ioutil.ReadFile("mytest.hcl")
	require.NoError(t, err)
	require.Equal(t, defaultHclVarSpec, string(content))
}

func TestVarInitCommand_Run_JSON(t *testing.T) {
	ci.Parallel(t)
	ui := cli.NewMockUi()
	cmd := &VarInitCommand{Meta: Meta{Ui: ui}}

	// Fails on misuse
	code := cmd.Run([]string{"some", "bad", "args"})
	require.Equal(t, 1, code)
	require.Contains(t, ui.ErrorWriter.String(), commandErrorText(cmd))
	ui.ErrorWriter.Reset()

	// Ensure we change the cwd back
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(origDir)

	// Create a temp dir and change into it
	dir := t.TempDir()

	err = os.Chdir(dir)
	require.NoError(t, err)

	// Works if the file doesn't exist
	code = cmd.Run([]string{"-json"})
	require.Contains(t, ui.ErrorWriter.String(), "REMINDER: While keys")
	require.Zero(t, code)

	content, err := ioutil.ReadFile(DefaultJsonVarInitName)
	require.NoError(t, err)
	require.Equal(t, defaultJsonVarSpec, string(content))

	// Fails if the file exists
	code = cmd.Run([]string{"-json"})
	require.Contains(t, ui.ErrorWriter.String(), "exists")
	require.Equal(t, 1, code)
	ui.ErrorWriter.Reset()

	// Works if file is passed
	code = cmd.Run([]string{"-json", "mytest.json"})
	require.Contains(t, ui.ErrorWriter.String(), "REMINDER: While keys")
	require.Zero(t, code)

	content, err = ioutil.ReadFile("mytest.json")
	require.NoError(t, err)
	require.Equal(t, defaultJsonVarSpec, string(content))
}

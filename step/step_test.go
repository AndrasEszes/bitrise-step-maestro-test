package step

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_DisablesMaestroAnalyticsAndUpdateCheck(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env.txt")
	fakeMaestro := filepath.Join(t.TempDir(), "maestro")
	writeFile(t, fakeMaestro, "#!/bin/sh\nenv > \""+envFile+"\"\n")
	require.NoError(t, os.Chmod(fakeMaestro, 0755))

	s := testStep()
	s.commandFactory = command.NewFactory(env.NewRepository())

	_, err := s.Run(Config{FlowPath: ".maestro"}, Installation{BinaryPath: fakeMaestro})
	require.NoError(t, err)

	maestroEnv, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Contains(t, string(maestroEnv), "MAESTRO_CLI_NO_ANALYTICS=true\n")
	assert.Contains(t, string(maestroEnv), "MAESTRO_DISABLE_UPDATE_CHECK=true\n")
	assert.Contains(t, string(maestroEnv), "PATH=", "the rest of the environment is passed through")
}

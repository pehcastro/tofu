package existential

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExists(t *testing.T) {
	got := Something()
	require.NotNil(t, got)
}

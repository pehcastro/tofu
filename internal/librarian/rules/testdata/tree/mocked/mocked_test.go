package mocked

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTheStoreWasAsked(t *testing.T) {
	asked := new(mock.Mock)
	assert.NotNil(t, asked)
	asked.AssertCalled(t, "Get")
}

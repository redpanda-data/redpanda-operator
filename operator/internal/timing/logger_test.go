package timing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestLogger(t *testing.T) {
	var buffer bytes.Buffer
	logger := zapLogger(&buffer)

	logger.Error(errors.New("test"), " | error | ")
	if logger.Enabled() {
		t.Error("logger should only be enabled for V(10), but was by default")
	}
	logger.Info(" | default | ")

	for i := 0; i < 100; i++ {
		shouldBeEnabled := i == 10
		logger.V(i).Info(fmt.Sprintf(" | %d | ", i))
		if logger.V(i).Enabled() && !shouldBeEnabled {
			t.Errorf("logger should only be enabled for V(10), but was for V(%d)", i)
		}
		if !logger.V(i).Enabled() && shouldBeEnabled {
			t.Error("logger should be enabled for V(10), but was not")
		}
	}

	payload := make(map[string]string)
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Errorf("output buffer should only have a single json entry but doesn't: %s", buffer.String())
	} else {
		if payload["msg"] != " | 10 | " {
			t.Errorf("output buffer doesn't contain expected values: %s", payload["msg"])
		}
	}
}

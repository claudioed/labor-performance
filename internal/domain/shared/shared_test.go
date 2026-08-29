package shared_test

import (
	"errors"
	"testing"

	"github.com/claudioed/labor-performance/internal/domain/shared"
)

func TestNewTaskType_Success(t *testing.T) {
	for _, v := range []string{"PICK", "PACK", "SLAM"} {
		got, err := shared.NewTaskType(v)
		if err != nil {
			t.Fatalf("NewTaskType(%q): %v", v, err)
		}
		if string(got) != v {
			t.Fatalf("NewTaskType(%q) = %q", v, got)
		}
	}
}

func TestNewTaskType_FailingPath(t *testing.T) {
	for _, v := range []string{"", "pick", "WALK", "PICKX"} {
		if _, err := shared.NewTaskType(v); !errors.Is(err, shared.ErrUnknownTaskType) {
			t.Fatalf("NewTaskType(%q) error = %v, want ErrUnknownTaskType", v, err)
		}
	}
}

func TestParseTaskTypeLenient(t *testing.T) {
	cases := map[string]shared.TaskType{
		"PICK":  shared.Pick,
		"PACK":  shared.Pack,
		"SLAM":  shared.Slam,
		"":      "",
		"walk":  "",
		"ROBOT": "",
	}
	for in, want := range cases {
		if got := shared.ParseTaskTypeLenient(in); got != want {
			t.Fatalf("ParseTaskTypeLenient(%q) = %q, want %q", in, got, want)
		}
	}
}

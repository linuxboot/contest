package job

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidVersion(t *testing.T) {
	validJd := Descriptor{
		Version: "1.0",
	}

	require.NoError(t, validJd.CheckVersion())
}

func TestEmptyVersion(t *testing.T) {
	emptyVersionJd := Descriptor{
		Version: "",
	}

	require.EqualError(
		t,
		emptyVersionJd.CheckVersion(),
		"Version Error: Empty Job Descriptor Version Field!",
	)
}

func TestInCompitableVersions(t *testing.T) {
	// CurrentVersion "1.0"
	cases := []struct {
		version        string
		expectedErrMsg string
	}{
		{"1.1", "Version Error: The Job Descriptor Version 1.1 is't compatible with the Server's 1.0"},
		{"2.0", "Version Error: The Job Descriptor Version 2.0 is't compatible with the Server's 1.0"},
	}

	for _, c := range cases {
		require.EqualError(
			t,
			(&Descriptor{Version: c.version}).CheckVersion(),
			c.expectedErrMsg,
		)
	}

}

func TestInValidVersion(t *testing.T) {
	// CurrentVersion "1.0"
	cases := []struct {
		version        string
		expectedErrMsg string
	}{
		{"1.", "Version Error: strconv.Atoi: parsing \"\": invalid syntax"},
		{".0", "Version Error: strconv.Atoi: parsing \"\": invalid syntax"},
		{".", "Version Error: strconv.Atoi: parsing \"\": invalid syntax"},
		{"1.a", "Version Error: strconv.Atoi: parsing \"a\": invalid syntax"},
		{"a.0", "Version Error: strconv.Atoi: parsing \"a\": invalid syntax"},
		{"123", "Version Error: Incorrect Job Descriptor Version 123"},
		{"abc", "Version Error: Incorrect Job Descriptor Version abc"},
	}

	for _, c := range cases {
		require.EqualError(
			t,
			(&Descriptor{Version: c.version}).CheckVersion(),
			c.expectedErrMsg,
		)
	}
}

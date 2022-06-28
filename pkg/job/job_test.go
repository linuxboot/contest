package job

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	currentDescriptorVersion = CurrentDescriptorVersion()
)

type Case struct {
	version        string
	expectedErrMsg string
}

func TestValidVersion(t *testing.T) {
	valid := Descriptor{
		Version: currentDescriptorVersion,
	}

	require.NoError(t, valid.CheckVersion())
}

func TestEmptyVersion(t *testing.T) {
	emptyVersionJD := Descriptor{
		Version: "",
	}

	require.EqualError(
		t,
		emptyVersionJD.CheckVersion(),
		"Version Error: Empty Job Descriptor Version Field!",
	)
}

func TestIncompatibleVersions(t *testing.T) {
	var cases []Case

	cases = append(cases, Case{
		fmt.Sprintf("%d.%d", JobDescriptorMajorVersion, JobDescriptorMinorVersion+1),
		"Version Error: The Job Descriptor Version %s is not compatible with the server: %s",
	})

	cases = append(cases, Case{
		fmt.Sprintf("%d.%d", JobDescriptorMajorVersion+1, JobDescriptorMinorVersion),
		"Version Error: The Job Descriptor Version %s is not compatible with the server: %s",
	})

	for _, c := range cases {
		require.EqualError(
			t,
			(&Descriptor{Version: c.version}).CheckVersion(),
			fmt.Sprintf(c.expectedErrMsg, c.version, currentDescriptorVersion),
		)
	}

}

func TestInvalidVersion(t *testing.T) {
	cases := []Case{
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

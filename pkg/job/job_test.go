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
		"version Error: Empty Job Descriptor Version Field",
	)
}

func TestIncompatibleVersions(t *testing.T) {
	var cases []Case

	cases = append(cases, Case{
		fmt.Sprintf("%d.%d", JobDescriptorMajorVersion, JobDescriptorMinorVersion+1),
		"version Error: The Job Descriptor Version %s is not compatible with the server: %s",
	})

	cases = append(cases, Case{
		fmt.Sprintf("%d.%d", JobDescriptorMajorVersion+1, JobDescriptorMinorVersion),
		"version Error: The Job Descriptor Version %s is not compatible with the server: %s",
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
		{"1.", "version Error: strconv.Atoi: parsing \"\": invalid syntax"},
		{".0", "version Error: strconv.Atoi: parsing \"\": invalid syntax"},
		{".", "version Error: strconv.Atoi: parsing \"\": invalid syntax"},
		{"1.a", "version Error: strconv.Atoi: parsing \"a\": invalid syntax"},
		{"a.0", "version Error: strconv.Atoi: parsing \"a\": invalid syntax"},
		{"123", "version Error: Incorrect Job Descriptor Version 123"},
		{"abc", "version Error: Incorrect Job Descriptor Version abc"},
	}

	for _, c := range cases {
		require.EqualError(
			t,
			(&Descriptor{Version: c.version}).CheckVersion(),
			c.expectedErrMsg,
		)
	}
}

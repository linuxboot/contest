package hwaas

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/linuxboot/contest/pkg/xcontext"
)

const (
	clearCRC = "clearcrc"
)

// flashCmds is a helper function to call into the different flash commands
func (ts *TestStep) keyboardCmds(ctx xcontext.Context, outputBuf *strings.Builder) error {
	if len(ts.Parameter.Args) >= 1 {
		switch ts.Parameter.Args[0] {

		case clearCRC:
			if err := ts.clearCRC(ctx, outputBuf); err != nil {
				return err
			}

			return nil

		default:
			return fmt.Errorf("failed to execute the keyboard command. The argument '%s' is not valid. Possible values are 'clearcrc'.", ts.Parameter.Args)
		}
	} else {
		return fmt.Errorf("failed to execute the keyboard command. Possible values are 'clearcrc'.")
	}
}

// clearCRC calls out /clearcrc endpoint and clears the 'bad crc' error.
func (ts *TestStep) clearCRC(ctx xcontext.Context, outputBuf *strings.Builder) error {
	endpoint := fmt.Sprintf("%s%s/contexts/%s/machines/%s/auxiliaries/%s/api/clearcrc",
		ts.Parameter.Host, ts.Parameter.Version, ts.Parameter.ContextID, ts.Parameter.MachineID, ts.Parameter.DeviceID)

	resp, err := HTTPRequest(ctx, http.MethodPost, endpoint, bytes.NewBuffer(nil))
	if err != nil {
		return fmt.Errorf("failed to do HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to clear bad crc error. Statuscode: %d", resp.StatusCode)
	}

	outputBuf.WriteString("Successfully cleared 'Bad CRC' Bios error.\n")

	return nil
}

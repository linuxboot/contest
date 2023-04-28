package hwaas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/linuxboot/contest/pkg/xcontext"
)

// flashWrite executes the flash write command.
func (p *Parameter) flashWrite(ctx xcontext.Context, arg string) error {
	log := ctx.Logger()

	if arg == "" {
		return fmt.Errorf("no file was set to read or write")
	}

	state, err := p.getState(ctx, "reset")
	if err != nil {
		return err
	}
	if state == "off" {
		// Than turn off the pdu, even if the graceful shutdown was not working
		statusCode, err := p.pressPDU(ctx, http.MethodDelete)
		if err != nil {
			return err
		}
		if statusCode == 200 {
			log.Infof("pdu powered off")
		} else {

			log.Infof("pdu could not be powered off")

			return fmt.Errorf("pdu could not be powered off")
		}

		// Than pull the reset switch on on
		statusCode, err = p.postReset(ctx, "on")
		if err != nil {
			return err
		}
		if statusCode == 200 {
			state, err = p.getState(ctx, "reset")
			if err != nil {
				return err
			}

			if state == "on" {
				log.Infof("reset is in state on")
			} else {
				return fmt.Errorf("reset switch could not be turned on")
			}
		} else {
			return fmt.Errorf("reset switch could not be turned on")
		}
	}

	endpoint := fmt.Sprintf("%s:%s/contexts/%s/machines/%s/auxiliaries/%s/api/flash", p.hostname, p.port, p.contextID, p.machineID, p.deviceID)

	targetInfo, err := getTargetState(ctx, endpoint)
	if err != nil {
		return err
	}
	if targetInfo.State == "busy" {
		return fmt.Errorf("target is currently busy")
	}
	if targetInfo.State == "error" {
		log.Infof("error from last flash: %s", targetInfo.Error)
	}

	err = flashTarget(ctx, endpoint, arg)
	if err != nil {
		return fmt.Errorf("flashing %s failed: %v\n", arg, err)
	}

	timestamp := time.Now()

	for {
		targetInfo, err := getTargetState(ctx, endpoint)
		if err != nil {
			return err
		}
		if targetInfo.State == "ready" {
			break
		}
		if targetInfo.State == "busy" {
			log.Infof("target is currently busy")
		}
		if targetInfo.State == "error" {
			log.Infof("error while flashing: %s", targetInfo.Error)
		}
		if time.Now().Sub(timestamp) >= defaultTimeoutParameter {
			return fmt.Errorf("flashing failed: timeout")
		}

		time.Sleep(time.Second)
	}

	log.Infof("successfully flashed binary")

	// Make device bootable again reset switch on off
	// Pull reset to off
	statusCode, err := p.postReset(ctx, "off")
	if err != nil {
		return err
	}
	if statusCode == 200 {
		state, err := p.getState(ctx, "reset")
		if err != nil {
			return err
		}

		if state == "off" {
			log.Infof("reset is in state off")
		} else {
			return fmt.Errorf("reset switch could not be turned off")
		}
	} else {
		return fmt.Errorf("reset switch could not be turned off")
	}

	time.Sleep(time.Second)

	// Than turn on the pdu again
	statusCode, err = p.pressPDU(ctx, http.MethodPut)
	if err != nil {
		return err
	}

	if statusCode == 200 {
		log.Infof("pdu powered on")
	} else {
		return fmt.Errorf("pdu could not be powered on")
	}

	return nil
}

// getTargetState returns the flash state of the target.
// If an error occured, the field error is filled.
func getTargetState(ctx xcontext.Context, endpoint string) (getFlash, error) {
	resp, err := HTTPRequest(ctx, http.MethodGet, endpoint, bytes.NewBuffer(nil))
	if err != nil {
		return getFlash{}, fmt.Errorf("failed to do http request: %v", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return getFlash{}, fmt.Errorf("could not extract response body: %v", err)
	}

	data := getFlash{}

	if err := json.Unmarshal(body, &data); err != nil {
		return getFlash{}, fmt.Errorf("could not unmarshal response body: %v", err)
	}

	return data, nil
}

// flashTarget flashes the target.
func flashTarget(ctx xcontext.Context, endpoint string, filePath string) error {
	file, _ := os.Open(filePath)
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	form, _ := writer.CreateFormFile("file", filepath.Base(filePath))
	io.Copy(form, file)
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s%s", endpoint, "/file"), body)
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", writer.FormDataContentType())

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to upload binary")
	}

	time.Sleep(20 * time.Second)

	postFlash := postFlash{
		Action: "write",
	}

	flashBody, err := json.Marshal(postFlash)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	resp, err = HTTPRequest(ctx, http.MethodPost, endpoint, bytes.NewBuffer(flashBody))
	if err != nil {
		return fmt.Errorf("failed to do http request")
	}

	if resp.StatusCode != 201 {
		return fmt.Errorf("failed to flash binary on target: %v: %v", resp.StatusCode, resp.Body)
	}

	return nil
}

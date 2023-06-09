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

	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/xcontext"
)

// flashWrite executes the flash write command.
func (p *Parameter) flashWrite(ctx xcontext.Context, arg string, target *target.Target, ev testevent.Emitter) error {
	log := ctx.Logger()

	var stdout string

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
		if statusCode == http.StatusOK {
			toStdout(ctx, &stdout, "pdu was set to state 'off'")
		} else {
			return fmt.Errorf("pdu could not set to state 'off'")
		}

		// Than pull the reset switch on on
		statusCode, err = p.postReset(ctx, "on")
		if err != nil {
			return err
		}
		if statusCode == http.StatusOK {
			state, err = p.getState(ctx, "reset")
			if err != nil {
				return err
			}

			if state == "on" {
				toStdout(ctx, &stdout, "reset is in state 'on'")
			} else {
				return fmt.Errorf("reset could not be set to state 'on'")
			}
		} else {
			return fmt.Errorf("reset could not be set to state 'on'")
		}
	}

	endpoint := fmt.Sprintf("%s:%s/contexts/%s/machines/%s/auxiliaries/%s/api/flash", p.host, p.port, p.contextID, p.machineID, p.deviceID)

	targetInfo, err := getTargetState(ctx, endpoint)
	if err != nil {
		return err
	}
	if targetInfo.State == "busy" {
		return fmt.Errorf("DUT is currently busy")
	}
	if targetInfo.State == "error" {
		log.Infof("error from last flash: %s", targetInfo.Error)
	}

	err = flashTarget(ctx, endpoint, arg)
	if err != nil {
		return fmt.Errorf("flashing DUT with %s failed: %v\n", arg, err)
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
			time.Sleep(time.Second)

			continue
		}
		if targetInfo.State == "error" {
			return fmt.Errorf("error while flashing DUT: %s", targetInfo.Error)
		}
		if time.Since(timestamp) >= defaultTimeoutParameter {
			return fmt.Errorf("flashing DUT failed: timeout")
		}
	}

	toStdout(ctx, &stdout, "successfully flashed binary on DUT")

	// Make device bootable again reset switch on off
	// Pull reset to off
	statusCode, err := p.postReset(ctx, "off")
	if err != nil {
		return err
	}
	if statusCode == http.StatusOK {
		state, err := p.getState(ctx, "reset")
		if err != nil {
			return err
		}

		if state == "off" {
			toStdout(ctx, &stdout, "reset was set to state 'off'")
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

	if statusCode == http.StatusOK {
		toStdout(ctx, &stdout, "pdu was set to state 'on'")
	} else {
		return fmt.Errorf("pdu could not be powered on")
	}

	time.Sleep(5 * time.Second)

	if p.emitStdout {
		log.Infof("Emitting stdout event")
		if err := emitEvent(ctx, EventStdout, eventStdoutPayload{Msg: stdout}, target, ev); err != nil {
			log.Warnf("Failed to emit event: %v", err)
		}
	}

	return nil
}

// flashRead executes the flash read command.
func (p *Parameter) flashRead(ctx xcontext.Context, arg string, target *target.Target, ev testevent.Emitter) error {
	log := ctx.Logger()

	var stdout string

	if arg == "" {
		return fmt.Errorf("no file was set to read or write")
	}

	endpoint := fmt.Sprintf("%s:%s/contexts/%s/machines/%s/auxiliaries/%s/api/flash", p.host, p.port, p.contextID, p.machineID, p.deviceID)

	targetInfo, err := getTargetState(ctx, endpoint)
	if err != nil {
		return err
	}
	if targetInfo.State == "busy" {
		return fmt.Errorf("DUT is currently busy")
	}
	if targetInfo.State == "error" {
		log.Infof("error from operation on flash: %s", targetInfo.Error)
	}

	err = loadTarget(ctx, endpoint, arg)
	if err != nil {
		return fmt.Errorf("loading binary image from DUT failed: %v\n", err)
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
			time.Sleep(time.Second)

			continue
		}
		if targetInfo.State == "error" {
			return fmt.Errorf("error while reading binary image from DUT: %s", targetInfo.Error)
		}
		if time.Since(timestamp) >= defaultTimeoutParameter {
			return fmt.Errorf("reading binary image from DUT failed: timeout")
		}
	}

	if err := readTarget(ctx, endpoint, arg); err != nil {
		return err
	}

	toStdout(ctx, &stdout, "binary image downloaded successfully")

	if p.emitStdout {
		log.Infof("Emitting stdout event")
		if err := emitEvent(ctx, EventStdout, eventStdoutPayload{Msg: stdout}, target, ev); err != nil {
			log.Warnf("Failed to emit event: %v", err)
		}
	}

	return nil
}

// getTargetState returns the flash state of the target.
// If an error occured, the field error is filled.
func getTargetState(ctx xcontext.Context, endpoint string) (getFlash, error) {
	log := ctx.Logger()

	resp, err := HTTPRequest(ctx, http.MethodGet, endpoint, bytes.NewBuffer(nil))
	if err != nil {
		return getFlash{}, fmt.Errorf("failed to do http request: %v", err)
	}
	defer resp.Body.Close()

	jsonBody, err := json.Marshal(resp.Body)
	if err != nil {
		return getFlash{}, fmt.Errorf("failed to marshal resp.Body: %v", err)
	}

	if ctx.Writer() != nil {
		writer := ctx.Writer()
		_, err := writer.Write(jsonBody)
		if err != nil {
			log.Warnf("writing to ctx.Writer failed: %w", err)
		}
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

// readTarget downloads the binary from the target and stores it at 'filePath'.
func readTarget(ctx xcontext.Context, endpoint string, filePath string) error {
	log := ctx.Logger()

	endpoint = fmt.Sprintf("%s%s", endpoint, "/file")

	resp, err := HTTPRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to do http request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download binary")
	}

	jsonBody, err := json.Marshal(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to marshal resp.Body: %v", err)
	}

	if ctx.Writer() != nil {
		writer := ctx.Writer()
		_, err := writer.Write(jsonBody)
		if err != nil {
			log.Warnf("writing to ctx.Writer failed: %w", err)
		}
	}

	// open/create file and copy the http response body into it
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open/create file at the provided path: %v", err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy binary to file: %v", err)
	}

	return nil
}

// loadTarget downloads the binary from the target and stores it at 'filePath'.
func loadTarget(ctx xcontext.Context, endpoint string, filePath string) error {
	log := ctx.Logger()

	postFlash := postFlash{
		Action: "read",
	}

	flashBody, err := json.Marshal(postFlash)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	resp, err := HTTPRequest(ctx, http.MethodPost, endpoint, bytes.NewBuffer(flashBody))
	if err != nil {
		return fmt.Errorf("failed to do http request")
	}

	jsonBody, err := json.Marshal(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to marshal resp.Body: %v", err)
	}

	if ctx.Writer() != nil {
		writer := ctx.Writer()
		_, err := writer.Write(jsonBody)
		if err != nil {
			log.Warnf("writing to ctx.Writer failed: %w", err)
		}
	}

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to load image on target: %v: %v", resp.StatusCode, resp.Body)
	}

	return nil
}

// flashTarget flashes the target.
func flashTarget(ctx xcontext.Context, endpoint string, filePath string) error {
	log := ctx.Logger()

	// open the binary that shall be flashed
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open the file at the provided path: %v", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	form, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("failed to create the form-data header: %v", err)
	}

	if _, err := io.Copy(form, file); err != nil {
		return fmt.Errorf("failed to copy file into form writer: %v", err)
	}

	writer.Close()

	// create the http request
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s%s", endpoint, "/file"), body)
	if err != nil {
		return fmt.Errorf("failed to create the http request: %v", err)
	}
	// add the file to the header
	req.Header.Add("Content-Type", writer.FormDataContentType())

	// execute the http request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to do the http request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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

	jsonBody, err := json.Marshal(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to marshal resp.Body: %v", err)
	}

	if ctx.Writer() != nil {
		writer := ctx.Writer()
		_, err := writer.Write(jsonBody)
		if err != nil {
			log.Warnf("writing to ctx.Writer failed: %w", err)
		}
	}

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to flash binary on target: %v: %v", resp.StatusCode, resp.Body)
	}

	return nil
}

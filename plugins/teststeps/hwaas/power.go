package hwaas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/linuxboot/contest/pkg/xcontext"
)

// powerOn turns on the device. To power the device on we have to fulfill this requirements -> reset is off -> pdu is on.
func (p *Parameter) powerOn(ctx xcontext.Context) error {
	log := ctx.Logger()

	// First pull reset switch on off
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

	if statusCode == http.StatusOK {
		log.Infof("pdu powered on")
	} else {
		return fmt.Errorf("pdu could not be powered on")
	}

	time.Sleep(time.Second)

	// Than press the power button
	statusCode, err = p.postPower(ctx, "3s")
	if err != nil {
		return err
	}
	if statusCode == http.StatusOK {
		log.Infof("dut is starting")

		time.Sleep(5 * time.Second)
	} else {
		return fmt.Errorf("device could not be turned on")
	}

	// Check the led if the device is on
	state, err := p.getState(ctx, "led")
	if err != nil {
		return err
	}

	if state != "on" {
		return fmt.Errorf("dut is not on")
	}

	return nil
}

// powerOffSoft turns off the device.
func (p *Parameter) powerOffSoft(ctx xcontext.Context) error {
	log := ctx.Logger()

	// First check if device needs to be powered down
	// Check the led if the device is on
	state, err := p.getState(ctx, "led")
	if err != nil {
		return err
	}

	if state == "on" {
		// If device is on, press power button for 12s
		statusCode, err := p.postPower(ctx, "12s")
		if err != nil {
			return err
		}
		if statusCode == http.StatusOK {
			log.Infof("dut is shutting down")

			time.Sleep(12 * time.Second)
		} else {
			log.Infof("dut is was not powered down gracefully")
		}
	}

	time.Sleep(time.Second)

	log.Infof("successfully powered down dut soft")

	return nil
}

// powerOffHard ensures that -> pdu is off & reset is on.
func (p *Parameter) powerOffHard(ctx xcontext.Context) error {
	log := ctx.Logger()

	// Than turn off the pdu, even if the graceful shutdown was not working
	statusCode, err := p.pressPDU(ctx, http.MethodDelete)
	if err != nil {
		return err
	}
	if statusCode == http.StatusOK {
		log.Infof("pdu powered off")
	} else {
		log.Infof("pdu could not be powered off")

		return fmt.Errorf("pdu could not be powered off")
	}

	time.Sleep(time.Second)

	// Than pull the reset switch on on
	statusCode, err = p.postReset(ctx, "on")
	if err != nil {
		return err
	}
	if statusCode == http.StatusOK {
		state, err := p.getState(ctx, "reset")
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

	log.Infof("successfully powered down dut hard")

	return nil
}

// postPower pushes the power button for the time of 'duration'.
// duration can be set from 0s to 20s.
func (p *Parameter) postPower(ctx xcontext.Context, duration string) (int, error) {
	endpoint := fmt.Sprintf("%s:%s/contexts/%s/machines/%s/auxiliaries/%s/api/power",
		p.hostname, p.port, p.contextID, p.machineID, p.deviceID)

	postPower := postPower{
		Duration: duration,
	}

	powerBody, err := json.Marshal(postPower)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal body: %w", err)
	}

	resp, err := HTTPRequest(ctx, http.MethodPost, endpoint, bytes.NewBuffer(powerBody))
	if err != nil {
		return 0, fmt.Errorf("failed to do http request: %v", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

// pressPDU toggles the PDU as you define the method input parameter.
// http.MethodDelete does power off the pdu.
// http.MethodPut does power on the pdu.
func (p *Parameter) pressPDU(ctx xcontext.Context, method string) (int, error) {
	if method != http.MethodDelete && method != http.MethodPut {
		return 0, fmt.Errorf("invalid method")
	}

	endpoint := fmt.Sprintf("%s:%s/contexts/%s/machines/%s/power", p.hostname, p.port, p.contextID, p.machineID)

	resp, err := HTTPRequest(ctx, method, endpoint, bytes.NewBuffer(nil))
	if err != nil {
		return 0, fmt.Errorf("failed to do http request: %v", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

// postReset toggles the Reset button regarding the state that is passed in.
// A valid state is either 'on' or 'off'.
func (p *Parameter) postReset(ctx xcontext.Context, state string) (int, error) {
	if state != "on" && state != "off" {
		return 0, fmt.Errorf("invalid state")
	}

	endpoint := fmt.Sprintf("%s:%s/contexts/%s/machines/%s/auxiliaries/%s/api/reset",
		p.hostname, p.port, p.contextID, p.machineID, p.deviceID)

	postReset := postReset{
		State: state,
	}

	resetBody, err := json.Marshal(postReset)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal body: %w", err)
	}

	resp, err := HTTPRequest(ctx, http.MethodPost, endpoint, bytes.NewBuffer(resetBody))
	if err != nil {
		return 0, fmt.Errorf("failed to do http request: %v", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

// getState returns the state of either: 'led', 'reset' or 'vcc'.
// The input parameter command should have one of this values.
func (p *Parameter) getState(ctx xcontext.Context, command string) (string, error) {
	endpoint := fmt.Sprintf("%s:%s/contexts/%s/machines/%s/auxiliaries/%s/api/%s",
		p.hostname, p.port, p.contextID, p.machineID, p.deviceID, command)

	resp, err := HTTPRequest(ctx, http.MethodGet, endpoint, bytes.NewBuffer(nil))
	if err != nil {
		return "", fmt.Errorf("failed to do http request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vcc pin status could not be retrieved")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("could not extract response body: %v", err)
	}

	data := getState{}

	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("could not unmarshal response body: %v", err)
	}

	return data.State, nil
}

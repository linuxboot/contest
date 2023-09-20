package cpu

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/firmwareci/system-suite/pkg/cpu"
)

const (
	bigger    = ">"
	smaller   = "<"
	equal     = "="
	between   = "-"
	colon     = ":"
	semicolon = ";"
	comma     = ","
)

type General struct {
	Option string `json:"option"`
	Value  string `json:"value"`
}

type Individual struct {
	CPUs   []int  `json:"cpus"`
	Option string `json:"option"`
	Value  string `json:"value"`
}

type Stats struct {
	Data cpu.Stats `json:"data"`
}

func (s *Stats) CheckGeneralOption(expect General, outputBuf *strings.Builder) error {
	switch expect.Option {
	case "CPUsLogical":
		cpusLogical, err := strconv.Atoi(expect.Value)
		if err != nil {
			err := fmt.Errorf("failed to convert input value for '%s' option: %v", expect.Option, err)
			outputBuf.WriteString(err.Error())

			return err
		}
		if s.Data.CPUsLogical != cpusLogical {
			err := fmt.Errorf("\u2717 %s is not as expected, have '%d', want '%d'", expect.Option, s.Data.CPUsLogical, cpusLogical)
			outputBuf.WriteString(err.Error())

			return err
		}

		outputBuf.WriteString(fmt.Sprintf("\u2713 %s is as expected. System has '%d' logical Cores.\n",
			expect.Option, s.Data.CPUsLogical))

	case "CPUsPhysical":
		cpusPhysical, err := strconv.Atoi(expect.Value)
		if err != nil {
			err := fmt.Errorf("failed to convert input value for '%s' option: %v", expect.Option, err)
			outputBuf.WriteString(err.Error())

			return err
		}
		if s.Data.CPUsPhysical != cpusPhysical {
			err := fmt.Errorf("\u2717 %s is not as expected, have '%d', want '%d'", expect.Option, s.Data.CPUsPhysical, cpusPhysical)
			outputBuf.WriteString(err.Error())

			return err
		}

		outputBuf.WriteString(fmt.Sprintf("\u2713 %s is as expected. System has '%d' physical Cores.\n",
			expect.Option, s.Data.CPUsPhysical))

	case "Profile":
		if s.Data.Profile != expect.Value {
			err := fmt.Errorf("\u2717 %s is not as expected, have '%s', want '%s'", expect.Option, s.Data.Profile, expect.Value)
			outputBuf.WriteString(err.Error())

			return err
		}

		outputBuf.WriteString(fmt.Sprintf("\u2713 %s is as expected. System has ACPI Platform Profile '%s'.\n",
			expect.Option, s.Data.Profile))

	case "CurPowerConsumption":
		rPowerCon := math.Round(s.Data.Power.CurPowerConsumption)
		if err := parseValue(int(rPowerCon), expect.Value); err != nil {
			err := fmt.Errorf("\u2717 %s is not as expected. Have '%dW', want '%sW'\n",
				expect.Option, int(rPowerCon), expect.Value)
			outputBuf.WriteString(err.Error())

			return err
		}

		outputBuf.WriteString(fmt.Sprintf("\u2713 %s is as expected. Have '%dW', want '%sW'\n",
			expect.Option, int(rPowerCon), expect.Value))

	case "PowerLimit1":
		if err := parseValue(s.Data.Power.PowerLimit1, expect.Value); err != nil {
			err := fmt.Errorf("error for option '%s': %v\n",
				expect.Option, err)
			outputBuf.WriteString(err.Error())

			return err
		}

		outputBuf.WriteString(fmt.Sprintf("\u2713 %s is as expected. Have '%dW', want '%sW'\n",
			expect.Option, s.Data.Power.PowerLimit1, expect.Value))

	case "PowerLimit2":
		if err := parseValue(s.Data.Power.PowerLimit2, expect.Value); err != nil {
			err := fmt.Errorf("\u2717 %s is not as expected. Have '%dW', want '%sW'\n",
				expect.Option, s.Data.Power.PowerLimit2, expect.Value)
			outputBuf.WriteString(err.Error())

			return err
		}

		outputBuf.WriteString(fmt.Sprintf("\u2713 %s is as expected. Have '%dW', want '%sW'\n",
			expect.Option, s.Data.Power.PowerLimit2, expect.Value))

	default:
		err := fmt.Errorf("\u2717 could to find option %s. Supported options are: '%s'", expect.Option, s.GeneralOptions())
		outputBuf.WriteString(err.Error())

		return err
	}

	return nil
}

func (s *Stats) CheckIndividualOption(expect Individual, interval bool, outputBuf *strings.Builder) error {
	var finalErr bool

	for _, cpu := range expect.CPUs {
		switch expect.Option {
		case "CStates":
			if err := s.checkCStatesFromString(cpu, expect, interval); err != nil {
				outputBuf.WriteString(fmt.Sprintf("\u2717 %s for CPU '%d' is not as expected:\n%s\n",
					expect.Option, cpu, err.Error()))

				finalErr = true

				continue
			}

			outputBuf.WriteString(fmt.Sprintf("\u2713 %s for CPU '%d' is as expected: '%s'\n",
				expect.Option, cpu, expect.Value))

		case "AverageFrequency":
			if err := parseValue(s.Data.CPUs[cpu].Frequency.AverageFrequency, expect.Value); err != nil {
				err := fmt.Errorf("\u2717 %s for CPU '%d' is not as expected:\n%s\n",
					expect.Option, cpu, err.Error())
				outputBuf.WriteString(err.Error())

				finalErr = true

				continue
			}

			outputBuf.WriteString(fmt.Sprintf("\u2713 %s for CPU '%d' is as expected: '%dKHz'.\n",
				expect.Option, cpu, s.Data.CPUs[cpu].Frequency.AverageFrequency))

		case "BusyFrequency":
			if err := parseValue(s.Data.CPUs[cpu].Frequency.BusyFrequency, expect.Value); err != nil {
				err := fmt.Errorf("\u2717 %s for CPU '%d' is not as expected:\n%s\n",
					expect.Option, cpu, err.Error())
				outputBuf.WriteString(err.Error())

				finalErr = true

				continue
			}

			outputBuf.WriteString(fmt.Sprintf("\u2713 %s for CPU '%d' is as expected: '%dKHz'.\n",
				expect.Option, cpu, s.Data.CPUs[cpu].Frequency.BusyFrequency))

		default:
			err := fmt.Errorf("\u2717 could not find option %s for CPU '%d'. Supported options are: '%s'\n",
				expect.Option, cpu, s.IndividualOptions())
			outputBuf.WriteString(err.Error())

			finalErr = true

			continue
		}
	}

	if finalErr {
		return fmt.Errorf("\u2717 error while checking individual option: %s", outputBuf.String())
	}

	return nil
}

func (s Stats) GeneralOptions() string {
	return fmt.Sprintf("%s, %s, %s, %s, %s, %s", "CPUsLogical", "CPUsPhysical", "Profile", "CurPowerConsumption",
		"PowerLimit1", "PowerLimit2")
}

func (s Stats) IndividualOptions() string {
	return fmt.Sprintf("%s, %s, %s", "CStates", "AverageFrequency", "BusyFrequency")
}

// parseCStatesFromString parses CState information from the expect value and compares the data regarding the real CState data.
func (s *Stats) checkCStatesFromString(cpu int, expect Individual, interval bool) error {
	const (
		individualRegex string = `^C\d+[A-Z]*:(<\d+|>\d+|\d+-\d+)(,C\d+[A-Z]*:(<\d+|>\d+|\d+-\d+))*$`
		groupRegex      string = `^C\d+[A-Z]*(,C\d+[A-Z]*)*:[<>]?\d*$`
	)

	var (
		errorString   string
		invalidFormat bool
	)

	// Split expect values into group or individual expects
	comparisons := strings.Split(expect.Value, semicolon)

	for _, cmp := range comparisons {
		// copy expect to just hand over the splitted expect value
		updatedExpect := expect
		updatedExpect.Value = cmp

		if regexp.MustCompile(individualRegex).MatchString(cmp) {
			if err := s.checkIndividualCStates(cpu, updatedExpect, interval); err != nil {
				errorString += fmt.Sprintf("%v", err)
			}
			continue
		} else if regexp.MustCompile(groupRegex).MatchString(cmp) {
			if err := s.checkGroupCStates(cpu, updatedExpect, interval); err != nil {
				errorString += fmt.Sprintf("%v", err)
			}
			continue
		}

		invalidFormat = true
	}

	if invalidFormat {
		return fmt.Errorf("invalid format: '%s'", expect.Value)
	}

	if errorString != "" {
		return fmt.Errorf("%s", errorString)
	}

	return nil
}

// checkIndividualCStates parses individual CState information from the given expect value and calculates if the real data matches the expected one.
func (s *Stats) checkIndividualCStates(cpu int, expect Individual, interval bool) error {
	var errorString string

	// Split the expect.Value by comma to get individual CState comparisons
	comparisons := strings.Split(expect.Value, comma)
	// Iterate through each individual CState comparison
	for _, cmp := range comparisons {
		// Split the comparison by colon to get CState name and expected value
		parts := strings.Split(cmp, colon)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format for individual C-state: '%s'\n", cmp)
		}

		// Trim spaces from the CState name and the expected value
		cstateName := strings.TrimSpace(parts[0])
		expected := strings.TrimSpace(parts[1])

		// Variable to keep track of whether the CState was found
		found := false

		// Iterate through each CState in the data to find the one with the matching name
		for _, cstate := range s.Data.CPUs[cpu].CStates {
			if cstate.Name == cstateName {
				found = true
				// Check the expected value for comparison operators (<, >, or range)
				if strings.Contains(expected, smaller) {
					// Parse the limit for < operator and compare usage percentage accordingly
					limit, err := strconv.Atoi(strings.TrimLeft(expected, smaller))
					if err != nil {
						errorString += fmt.Sprintf("failed to parse value for '%s': %v\n", cstateName, err)
						continue
					}

					if !(cstate.UsagePercentage < float64(limit)) {
						errorString += fmt.Sprintf("Value for CState '%s' is not as expected. Want '%s%%' have '%f%%'.\n",
							cstate.Name, expected, cstate.UsagePercentage)
						continue
					}

				} else if strings.Contains(expected, bigger) {
					// Parse the limit for > operator and compare usage percentage accordingly
					limit, err := strconv.Atoi(strings.TrimLeft(expected, bigger))
					if err != nil {
						errorString += fmt.Sprintf("failed to parse value for '%s': %v\n", cstateName, err)
						continue
					}

					if !(cstate.UsagePercentage > float64(limit)) {
						errorString += fmt.Sprintf("Value for CState '%s' is not as expected. Want '%s%%' have '%f%%'.\n",
							cstate.Name, expected, cstate.UsagePercentage)
						continue
					}

				} else if strings.Contains(expected, between) {
					// Parse the limits for range and compare usage percentage accordingly
					limits := strings.Split(expected, between)
					if len(limits) != 2 {
						errorString += fmt.Sprintf("invalid range for '%s': '%s'\n", cstateName, expected)
						continue
					}

					minLimit, err := strconv.Atoi(strings.TrimSpace(limits[0]))
					if err != nil {
						errorString += fmt.Sprintf("failed to parse minimum value for '%s': %v\n", cstateName, err)
						continue
					}
					maxLimit, err := strconv.Atoi(strings.TrimSpace(limits[1]))
					if err != nil {
						errorString += fmt.Sprintf("failed to parse maximum value for '%s': %v\n", cstateName, err)
						continue
					}

					if !(cstate.UsagePercentage < float64(minLimit) && cstate.UsagePercentage > float64(maxLimit)) {
						errorString += fmt.Sprintf("Value for CState '%s' is not as expected. Want '%s%%' have '%f%%'.\n",
							cstate.Name, expected, cstate.UsagePercentage)
						continue
					}
				}
			}
		}

		// If CState is not found, add an error message for it
		if !found {
			errorString += fmt.Sprintf("CState with name '%s' not found.\n", cstateName)
		}
	}

	// If there are any error messages in errorString, return an error with the aggregated messages
	if errorString != "" {
		return fmt.Errorf("%s", errorString)
	}

	return nil
}

// parseIndividualCStates parses group CState information from the given expect value and calculates if the real data matches the expected one.
func (s *Stats) checkGroupCStates(cpu int, expect Individual, interval bool) error {
	// Split the expect.Value by colon to get CState names and the expected value
	cmpParts := strings.Split(expect.Value, colon)
	if len(cmpParts) != 2 {
		return fmt.Errorf("invalid format for group C-state: '%s'\n", expect.Value)
	}

	// Split the CState names by comma
	cstateNames := strings.Split(cmpParts[0], comma)
	if len(cstateNames) == 0 {
		return fmt.Errorf("no C-states specified in group comparison: '%s'\n", expect.Value)
	}

	// Trim spaces from the expected value
	expected := strings.TrimSpace(cmpParts[1])

	var totalPercentage float64

	var errorString string
	// Iterate through each CState in the group
	for _, cstateName := range cstateNames {
		found := false

		// Find the corresponding CState in the data
		for _, cstate := range s.Data.CPUs[cpu].CStates {
			if cstate.Name == cstateName {
				found = true
				// Add the CState's percentages
				totalPercentage += cstate.UsagePercentage
			}
		}

		// If CState is not found, add an error message for it
		if !found {
			errorString += fmt.Sprintf("CState with name '%s' not found.\n", cstateName)
		}
	}

	// If there are any error messages in errorString, return an error with the aggregated messages
	if errorString != "" {
		return fmt.Errorf("%s", errorString)
	}

	// Check the expected value for comparison operators (<, >, or range)
	if strings.Contains(expected, smaller) {
		// Parse the limit for < operator and compare the total percentage accordingly
		limit, err := strconv.Atoi(strings.TrimLeft(expected, smaller))
		if err != nil {
			return fmt.Errorf("failed to parse value for '%s': %v\n", cstateNames, err)
		}

		if !(totalPercentage < float64(limit)) {
			return fmt.Errorf("Value for CStates '%v' is not as expected. Want '%s%%' have '%f%%'.\n",
				cstateNames, expected, totalPercentage)
		}
	} else if strings.Contains(expected, bigger) {
		// Parse the limit for > operator and compare the total percentage accordingly
		limit, err := strconv.Atoi(strings.TrimLeft(expected, bigger))
		if err != nil {
			return fmt.Errorf("failed to parse value for '%s': %v", cstateNames, err)
		}

		if !(totalPercentage > float64(limit)) {
			return fmt.Errorf("Value for CStates '%v' is not as expected. Want '%s%%' have '%f%%'.\n",
				cstateNames, expected, totalPercentage)
		}
	} else if strings.Contains(expected, between) {
		// Parse the limits for range and compare the total percentage accordingly
		limits := strings.Split(expected, between)
		if len(limits) != 2 {
			return fmt.Errorf("invalid range for '%s': '%s'\n", cstateNames, expected)
		}

		minLimit, err := strconv.Atoi(strings.TrimSpace(limits[0]))
		if err != nil {
			return fmt.Errorf("failed to parse minimum value for '%s': %v\n", cstateNames, err)
		}

		maxLimit, err := strconv.Atoi(strings.TrimSpace(limits[1]))
		if err != nil {
			return fmt.Errorf("failed to parse maximum value for '%s': %v\n", cstateNames, err)
		}

		if !(totalPercentage < float64(minLimit) && totalPercentage > float64(maxLimit)) {
			return fmt.Errorf("Value for CStates '%v' is not as expected. Want '%s%%' have '%f%%'.\n",
				cstateNames, expected, totalPercentage)
		}
	}

	return nil
}

// parseValue parses an integer value 'have' against an expected pattern 'expect' and checks if the 'have' value satisfies the expectation.
// The function supports three cases for the 'expect' pattern: "<30" (value should be less than 30), ">30" (value should be greater than 30),
// and "30-40" (value should be between 30 and 40, inclusive).
func parseValue(have int, expect string) error {
	match, _ := regexp.MatchString(`^(\d+)-(\d+)$`, expect)
	if match {
		// Case for between 2 numbers.
		regex := regexp.MustCompile(`^(\d+)-(\d+)$`)
		match := regex.FindStringSubmatch(expect)

		min, err := strconv.Atoi(match[1])
		if err != nil {
			return fmt.Errorf("failed to convert '%s' into a number: %v", expect, err)
		}

		max, err := strconv.Atoi(match[2])
		if err != nil {
			return fmt.Errorf("failed to convert '%s' into a number: %v", expect, err)
		}

		if have < min || have > max {
			return fmt.Errorf("the value is not in expected range, have: '%d', want: '%d-%d'", have, min, max)
		}
	} else {
		// Cases for below and above.
		regex := regexp.MustCompile(`^([<>=])(\d+)$`)
		match := regex.FindStringSubmatch(expect)

		if len(match) > 1 {
			operator := match[1]
			value, err := strconv.Atoi(match[2])
			if err != nil {
				return fmt.Errorf("failed to convert '%s' into a number: %v", expect, err)
			}

			switch operator {
			case smaller:
				if have > value {
					return fmt.Errorf("value is not as expected, have: '%d', want: %s '%d'", have, operator, value)
				}
			case bigger:
				if have < value {
					return fmt.Errorf("value is not as expected, have: '%d', want: %s '%d'", have, operator, value)
				}
			case equal:
				if have != value {
					return fmt.Errorf("value is not as expected, have: '%d', want: %s '%d'", have, operator, value)
				}
			default:
				return fmt.Errorf("wrong operator, valid operators are '%s', '%s' and '%s'", smaller, bigger, equal)
			}
		} else {
			return fmt.Errorf("Value seems to be malformed.")
		}

	}

	return nil
}

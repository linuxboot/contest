package sshcopy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/linuxboot/contest/pkg/event"
	"github.com/linuxboot/contest/pkg/event/testevent"
	"github.com/linuxboot/contest/pkg/multiwriter"
	"github.com/linuxboot/contest/pkg/target"
	"github.com/linuxboot/contest/pkg/test"
	"github.com/linuxboot/contest/pkg/xcontext"
	"github.com/linuxboot/contest/plugins/teststeps"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Name is the name used to look this plugin up.
var Name = "SSHCopy"

// Events is used by the framework to determine which events this plugin will
// emit. Any emitted event that is not registered here will cause the plugin to
// fail.
var Events = []event.Name{}

const (
	defaultSSHPort          = 22
	defaultTimeoutParameter = "10m"
)

// SSHCopy is used to copy a file over ssh to a destination.
type SSHCopy struct {
	Host            *test.Param
	Port            *test.Param
	User            *test.Param
	PrivateKeyFile  *test.Param
	Password        *test.Param
	DstPath         *test.Param
	SrcPath         *test.Param
	Recursive       *test.Param
	Timeout         *test.Param
	SkipIfEmptyHost *test.Param
}

// Name returns the plugin name.
func (scp SSHCopy) Name() string {
	return Name
}

// Run executes the cmd step.
func (scp *SSHCopy) Run(ctx xcontext.Context, ch test.TestStepChannels, params test.TestStepParameters, ev testevent.Emitter, resumeState json.RawMessage) (json.RawMessage, error) {
	log := ctx.Logger()

	// XXX: Dragons ahead! The target (%t) substitution, and function
	// expression evaluations are done at run-time, so they may still fail
	// despite passing at early validation time.
	// If the function evaluations called in validateAndPopulate are not idempotent,
	// the output of the function expressions may be different (e.g. with a call to a
	// backend or a random pool of results)
	// Function evaluation could be done at validation time, but target
	// substitution cannot, because the targets are not known at that time.
	if err := scp.validateAndPopulate(params); err != nil {
		return nil, err
	}

	f := func(ctx xcontext.Context, target *target.Target) error {
		// apply filters and substitutions to user, host, private key, and command args
		user, err := scp.User.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand user parameter: %v", err)
		}

		host, err := scp.Host.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand host parameter: %v", err)
		}

		if len(host) == 0 {
			shouldSkip := false
			if !scp.SkipIfEmptyHost.IsEmpty() {
				var err error
				shouldSkip, err = strconv.ParseBool(scp.SkipIfEmptyHost.String())
				if err != nil {
					return fmt.Errorf("cannot expand 'skip_if_empty_host' parameter value '%s': %w", scp.SkipIfEmptyHost, err)
				}
			}

			if shouldSkip {
				return nil
			} else {
				return fmt.Errorf("host value is empty")
			}
		}

		portStr, err := scp.Port.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand port parameter: %v", err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return fmt.Errorf("failed to convert port parameter to integer: %v", err)
		}

		// apply functions to the private key, if any
		var signer ssh.Signer
		privKeyFile, err := scp.PrivateKeyFile.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand private key file parameter: %v", err)
		}

		if privKeyFile != "" {
			key, err := ioutil.ReadFile(privKeyFile)
			if err != nil {
				return fmt.Errorf("cannot read private key at %s: %v", scp.PrivateKeyFile, err)
			}
			signer, err = ssh.ParsePrivateKey(key)
			if err != nil {
				return fmt.Errorf("cannot parse private key: %v", err)
			}
		}

		password, err := scp.Password.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand password parameter: %v", err)
		}

		sourcePath, err := scp.SrcPath.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand source path parameter: %v", err)
		}

		destinationPath, err := scp.DstPath.Expand(target)
		if err != nil {
			return fmt.Errorf("cannot expand destination path parameter: %v", err)
		}

		recursive := false

		if scp.Recursive.IsEmpty() {
			recursive = false
		} else {
			recParam, err := scp.Recursive.Expand(target)
			if err != nil {
				return fmt.Errorf("cannot expand recursive parameter: %v", err)
			}
			if recParam != "true" && recParam != "false" {
				return fmt.Errorf("if you provide the recursive parameter, the only values supported are 'true' and 'false'")
			}
			if recParam == "true" {
				recursive = true
			}
		}

		auth := []ssh.AuthMethod{}
		if signer != nil {
			auth = append(auth, ssh.PublicKeys(signer))
		}
		if password != "" {
			auth = append(auth, ssh.Password(password))
		}

		config := ssh.ClientConfig{
			User: user,
			Auth: auth,
			// TODO expose this in the plugin arguments
			// HostKeyCallback: ssh.FixedHostKey(hostKey),
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}

		// connect to the host
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		sshClient, err := ssh.Dial("tcp", addr, &config)
		if err != nil {
			return fmt.Errorf("cannot connect to SSH server %s: %v", addr, err)
		}
		defer func() {
			if err := sshClient.Close(); err != nil {
				ctx.Warnf("Failed to close SSH connection to %s: %v", addr, err)
			}
		}()

		sftpClient, err := sftp.NewClient(sshClient)
		if err != nil {
			return fmt.Errorf("cannot create an new sftp client on top of the SSH connection: %v", err)
		}
		defer sftpClient.Close()

		if recursive {
			if err := filepath.Walk(sourcePath, func(srcPath string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				// we don't want to copy dirs and hidden files
				if info.IsDir() || filepath.Base(srcPath)[0] == '.' {
					return nil
				}

				// create directory if it does not exist
				dstDir := filepath.Dir(destinationPath + srcPath[len(sourcePath):])
				err = sftpClient.MkdirAll(dstDir)
				if err != nil {
					return err
				}

				// Create remote file
				dstFile, err := sftpClient.Create(destinationPath + srcPath[len(sourcePath):])
				if err != nil {
					return err
				}
				defer dstFile.Close()

				// Copy local file contents to remote file
				srcFile, err := os.Open(srcPath)
				if err != nil {
					return err
				}
				defer srcFile.Close()

				_, err = io.Copy(dstFile, srcFile)
				if err != nil {
					return err
				}

				return nil
			}); err != nil {
				return fmt.Errorf("failed to copy source file to destination recursively: %v", err)
			}

		} else {
			// open source file
			sourceFile, err := os.Open(sourcePath)
			if err != nil {
				return fmt.Errorf("failed to open the provided source file: %v", err)
			}
			defer sourceFile.Close()

			// create destination file
			destinationFile, err := sftpClient.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
			if err != nil {
				return fmt.Errorf("failed to open the provided source file: %v", err)
			}
			defer destinationFile.Close()

			// copy source content to destination
			_, err = io.Copy(destinationFile, sourceFile)
			if err != nil {
				return fmt.Errorf("failed to copy source file to destination: %v", err)
			}
		}

		// run the remote command and catch stdout/stderr
		var stdout bytes.Buffer

		mw := multiwriter.NewMultiWriter()
		if ctx.Writer() != nil {
			mw.AddWriter(ctx.Writer())
		}
		mw.AddWriter(&stdout)

		log.Infof("successful copied data to ssh destination")

		return nil
	}

	return teststeps.ForEachTarget(Name, ctx, ch, f)
}

func (scp *SSHCopy) validateAndPopulate(params test.TestStepParameters) error {
	var err error
	scp.Host = params.GetOne("host")
	if scp.Host.IsEmpty() {
		return errors.New("invalid or missing 'host' parameter, must be exactly one string")
	}
	if params.GetOne("port").IsEmpty() {
		scp.Port = test.NewParam(strconv.Itoa(defaultSSHPort))
	} else {
		var port int64
		port, err = params.GetInt("port")
		if err != nil {
			return fmt.Errorf("invalid 'port' parameter, not an integer: %v", err)
		}
		if port < 0 || port > 0xffff {
			return fmt.Errorf("invalid 'port' parameter: not in range 0-65535")
		}
	}

	scp.User = params.GetOne("user")
	if scp.User.IsEmpty() {
		return errors.New("invalid or missing 'user' parameter, must be exactly one string")
	}

	// do not fail if key file is empty, in such case it won't be used
	scp.PrivateKeyFile = params.GetOne("private_key_file")

	// do not fail if password is empty, in such case it won't be used
	scp.Password = params.GetOne("password")

	scp.SrcPath = params.GetOne("source")

	scp.DstPath = params.GetOne("destination")

	scp.Recursive = params.GetOne("recursive")

	scp.SkipIfEmptyHost = params.GetOne("skip_if_empty_host")
	return nil
}

// ValidateParameters validates the parameters associated to the TestStep
func (scp *SSHCopy) ValidateParameters(ctx xcontext.Context, params test.TestStepParameters) error {
	ctx.Debugf("Params %+v", params)
	return scp.validateAndPopulate(params)
}

// New initializes and returns a new SSHCmd test step.
func New() test.TestStep {
	return &SSHCopy{}
}

// Load returns the name, factory and events which are needed to register the step.
func Load() (string, test.TestStepFactory, []event.Name) {
	return Name, New, Events
}

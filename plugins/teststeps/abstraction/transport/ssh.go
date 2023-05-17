package transport

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"strconv"
	"time"

	"github.com/insomniacslk/xjson"
	"github.com/kballard/go-shellquote"
	"github.com/linuxboot/contest/pkg/xcontext"
	"golang.org/x/crypto/ssh"
)

type SSHTransportConfig struct {
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`

	User         string `json:"user,omitempty"`
	Password     string `json:"password,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`

	Timeout xjson.Duration `json:"timeout,omitempty"`
}

func DefaultSSHTransportConfig() SSHTransportConfig {
	return SSHTransportConfig{
		Port:    22,
		Timeout: xjson.Duration(10 * time.Minute),
	}
}

type SSHTransport struct {
	SSHTransportConfig
}

func NewSSHTransport(config SSHTransportConfig) Transport {
	return &SSHTransport{config}
}

func (st *SSHTransport) NewProcess(ctx xcontext.Context, bin string, args []string) (Process, error) {
	var signer ssh.Signer
	if st.IdentityFile != "" {
		key, err := ioutil.ReadFile(st.IdentityFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read private key at %s: %v", st.IdentityFile, err)
		}
		signer, err = ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("cannot parse private key: %v", err)
		}
	}

	auth := []ssh.AuthMethod{}
	if signer != nil {
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if st.Password != "" {
		auth = append(auth, ssh.Password(st.Password))
	}

	addr := net.JoinHostPort(st.Host, strconv.Itoa(st.Port))
	clientConfig := &ssh.ClientConfig{
		User: st.User,
		Auth: auth,
		// TODO expose this in the plugin arguments
		//HostKeyCallback: ssh.FixedHostKey(hostKey),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(st.Timeout),
	}

	// stack mechanism similar to defer, but run after the exec process ends
	stack := newDeferedStack()

	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to SSH server %s: %v", addr, err)
	}

	// cleanup the ssh client after the operations have ended
	stack.Add(func() {
		if err := client.Close(); err != nil {
			ctx.Warnf("failed to close SSH client: %v", err)
		}
	})

	return st.new(ctx, client, bin, args, stack)
}

func (st *SSHTransport) new(ctx xcontext.Context, client *ssh.Client, bin string, args []string, stack *deferedStack) (Process, error) {
	return newSSHProcess(ctx, client, bin, args, stack)
}

type sshProcess struct {
	session       *ssh.Session
	cmd           string
	keepAliveDone chan struct{}

	stack *deferedStack
}

func newSSHProcess(ctx xcontext.Context, client *ssh.Client, bin string, args []string, stack *deferedStack) (Process, error) {
	var stdin bytes.Buffer
	return newSSHProcessWithStdin(ctx, client, bin, args, &stdin, stack)
}

func newSSHProcessWithStdin(
	ctx xcontext.Context, client *ssh.Client,
	bin string, args []string,
	stdin io.Reader,
	stack *deferedStack,
) (Process, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("cannot create SSH session to server: %v", err)
	}

	// set fds for the remote process
	session.Stdin = stdin

	cmd := shellquote.Join(append([]string{bin}, args...)...)
	keepAliveDone := make(chan struct{})

	return &sshProcess{session, cmd, keepAliveDone, stack}, nil
}

func (sp *sshProcess) Start(ctx xcontext.Context) error {
	ctx.Debugf("starting remote binary: %s", sp.cmd)

	if err := sp.session.Start(sp.cmd); err != nil {
		return fmt.Errorf("failed to start process: %v", err)
	}

	go func() {
		for {
			select {
			case <-sp.keepAliveDone:
				return

			case <-time.After(5 * time.Second):
				ctx.Debugf("sending sigcont to ssh server...")
				if err := sp.session.Signal(ssh.Signal("CONT")); err != nil {
					ctx.Warnf("failed to send CONT to ssh server: %v", err)
				}

			case <-ctx.Done():
				ctx.Debugf("killing ssh session because of cancellation...")

				// TODO:  figure out if there's a way to fix this (can be used for resource exhaustion)
				// note: not all servers implement the signal message so this might
				// not do anything; see comment about cancellation in Wait()
				if err := sp.session.Signal(ssh.SIGKILL); err != nil {
					ctx.Warnf("failed to send KILL on context cancel: %v", err)
				}

				sp.session.Close()
				return
			}
		}
	}()

	return nil
}

func (sp *sshProcess) Wait(ctx xcontext.Context) error {
	// close these no matter what error we get from the wait
	defer func() {
		sp.stack.Done()
		close(sp.keepAliveDone)
	}()
	defer sp.session.Close()

	errChan := make(chan error, 1)
	go func() {
		if err := sp.session.Wait(); err != nil {
			var e *ssh.ExitError
			if errors.As(err, &e) {
				errChan <- &ExitError{e.ExitStatus()}
				return
			}

			errChan <- fmt.Errorf("failed to wait on process: %v", err)
		}
		errChan <- nil
	}()

	select {
	case <-ctx.Done():
		// cancellation was requested, a kill signal should've been sent but not
		// all ssh server implementations respect that, so in the worst case scenario
		// we just disconnect the ssh and leave the remote process to terminate by
		// itself (pid is also unavailable thru the ssh spec)

		// leave the process some time to exit in case the signal did work
		select {
		case <-time.After(3 * time.Second):
			return ctx.Err()

		case err := <-errChan:
			return err
		}

	case err := <-errChan:
		return err
	}
}

func (sp *sshProcess) StdoutPipe() (io.Reader, error) {
	stdout, err := sp.session.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %v", err)
	}

	return stdout, nil
}

func (sp *sshProcess) StderrPipe() (io.Reader, error) {
	stderr, err := sp.session.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %v", err)
	}

	return stderr, nil
}

func (sp *sshProcess) String() string {
	return sp.cmd
}

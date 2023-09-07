package transport

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/linuxboot/contest/pkg/xcontext"
)

type LocalTransport struct{}

func NewLocalTransport() Transport {
	return &LocalTransport{}
}

func (lt *LocalTransport) NewProcess(ctx xcontext.Context, bin string, args []string, workingDir string) (Process, error) {
	return newLocalProcess(ctx, bin, args, workingDir)
}

// localProcess is just a thin layer over exec.Command
type localProcess struct {
	cmd *exec.Cmd
}

func newLocalProcess(ctx xcontext.Context, bin string, args []string, workingDir string) (Process, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, err
	}
	if err := checkBinary(path); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = workingDir

	return &localProcess{cmd}, nil
}

func (lp *localProcess) Start(ctx xcontext.Context) error {
	ctx.Debugf("starting local binary: %v", lp)
	if err := lp.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	return nil
}

func (lp *localProcess) Wait(_ xcontext.Context) error {
	if err := lp.cmd.Wait(); err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) {
			return &ExitError{e.ExitCode()}
		}

		return fmt.Errorf("failed to wait on process: %w", err)
	}

	return nil
}

func (lp *localProcess) StdoutPipe() (io.Reader, error) {
	stdout, err := lp.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe")
	}
	return stdout, nil
}

func (lp *localProcess) StderrPipe() (io.Reader, error) {
	stderr, err := lp.cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe")
	}
	return stderr, nil
}

func (lp *localProcess) String() string {
	return lp.cmd.String()
}

// localCopy is just a thin layer over exec.Command
type localCopy struct {
	src       string
	dst       string
	recursive bool
}

func (lt *LocalTransport) NewCopy(ctx xcontext.Context, src, dst string, recursive bool) (Copy, error) {
	return &localCopy{src: src, dst: dst, recursive: recursive}, nil
}

func (lc *localCopy) Copy(ctx xcontext.Context) error {
	srcInfo, err := os.Stat(lc.src)
	if err != nil {
		return fmt.Errorf("failed to get source info: %w", err)
	}

	if srcInfo.IsDir() {
		if !lc.recursive {
			return fmt.Errorf("source is a directory, recursive copy is required")
		}
		return copyDirectory(lc.src, lc.dst)
	}

	return copyFile(lc.src, lc.dst)
}

func copyDirectory(srcDir, dstDir string) error {
	// Create the destination directory
	err := os.MkdirAll(dstDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Get a list of files/directories in the source directory
	files, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	// Copy each file/directory in the source directory
	for _, file := range files {
		srcPath := filepath.Join(srcDir, file.Name())
		dstPath := filepath.Join(dstDir, file.Name())

		if file.IsDir() {
			err = copyDirectory(srcPath, dstPath)
			if err != nil {
				return err
			}
		} else {
			err = copyFile(srcPath, dstPath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open the provided source file: %v", err)
	}
	defer srcFile.Close()

	// Get the file permissions of the source file
	srcFileInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcFileInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to open the provided source file: %v", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy source file to destination: %v", err)
	}

	return nil
}

func (lc *localCopy) String() string {
	if lc.recursive {
		return fmt.Sprintf("cp -r %s %s", lc.src, lc.dst)
	} else {
		return fmt.Sprintf("cp %s %s", lc.src, lc.dst)
	}
}

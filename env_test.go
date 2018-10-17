package tableflip

import (
	"os"
)

func testEnv() (*env, chan *testProcess) {
	procs := make(chan *testProcess, 10)
	return &env{
		newProc: func(_ string, _ []string, files []*os.File, env []string) (process, error) {
			p, err := newTestProcess(files, env)
			if err != nil {
				return nil, err
			}
			procs <- p
			return p, nil
		},
		environ:     func() []string { return nil },
		getenv:      func(string) string { return "" },
		closeOnExec: func(fd int) {},
	}, procs
}

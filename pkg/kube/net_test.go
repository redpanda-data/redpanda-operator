package kube_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientWriteToClosed(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	doneCh := make(chan struct{})
	connClosedCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		defer lis.Close()

		conn, err := lis.Accept()
		require.NoError(t, err)

		require.NoError(t, conn.Close())

		close(connClosedCh)
	}()

	conn, err := net.Dial("tcp", lis.Addr().String())
	require.NoError(t, err)

	<-connClosedCh
	for err == nil {
		_, err = conn.Write([]byte("hello world!"))
	}
	// net_test.go:36: Error writing to closed *net.TCPConn:
	//             *net.OpError: write tcp 127.0.0.1:60685->127.0.0.1:60684: write: broken pipe
	t.Logf("Error writing to closed %T:\n\t%T: %#v", conn, err, err)
	require.Error(t, err)
	<-doneCh
}

func TestClientReadFromClosed(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	doneCh := make(chan struct{})
	connClosedCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		defer lis.Close()

		conn, err := lis.Accept()
		require.NoError(t, err)

		require.NoError(t, conn.Close())

		close(connClosedCh)
	}()

	conn, err := net.Dial("tcp", lis.Addr().String())
	require.NoError(t, err)

	<-connClosedCh
	_, err = conn.Read(make([]byte, 1024))
	// net_test.go:69: Error reading from closed *net.TCPConn:
	//             *errors.errorString: &errors.errorString{s:"EOF"}
	t.Logf("Error reading from closed %T:\n\t%T: %#v", conn, err, err)
	require.Error(t, err)
	<-doneCh
}

func TestClientConnectToClosed(t *testing.T) {
	_, err := net.Dial("tcp", "127.0.0.1:47919")
	// net_test.go:77: Error connecting to non-listening port:
	//             *net.OpError: &net.OpError{Op:"dial", Net:"tcp", Source:net.Addr(nil), Addr:(*net.TCPAddr)(0x14000378c00), Err:(*os.SyscallError)(0x14000338120)}
	t.Logf("Error connecting to non-listening port:\n\t%T: %#v", err, err)
	require.Error(t, err)
}

func TestClientMultipleCloses(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	closedCh := make(chan struct{})
	go func() {
		conn, err := lis.Accept()
		require.NoError(t, err)

		<-closedCh

		require.NoError(t, conn.Close())

	}()

	conn, err := net.Dial("tcp", lis.Addr().String())
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		err = conn.Close()
		// net_test.go:101: Error closing *net.TCPConn:
		//             <nil>: <nil>
		// net_test.go:101: Error closing *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x140004ffe60), Addr:(*net.TCPAddr)(0x140004ffe90), Err:poll.errNetClosing{}}
		// net_test.go:101: Error closing *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x140004ffe60), Addr:(*net.TCPAddr)(0x140004ffe90), Err:poll.errNetClosing{}}
		// net_test.go:101: Error closing *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x140004ffe60), Addr:(*net.TCPAddr)(0x140004ffe90), Err:poll.errNetClosing{}}
		// net_test.go:101: Error closing *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x140004ffe60), Addr:(*net.TCPAddr)(0x140004ffe90), Err:poll.errNetClosing{}}
		t.Logf("Error closing %T:\n\t%T: %#v", conn, err, err)
	}

	close(closedCh)
}

func TestClientCloseOfClosed(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	closedCh := make(chan struct{})
	go func() {
		conn, err := lis.Accept()
		require.NoError(t, err)

		require.NoError(t, conn.Close())

		close(closedCh)
	}()

	conn, err := net.Dial("tcp", lis.Addr().String())
	require.NoError(t, err)

	<-closedCh

	for i := 0; i < 5; i++ {
		err = conn.Close()
		// net_test.go:138: Error closing closed *net.TCPConn:
		//             <nil>: <nil>
		// net_test.go:138: Error closing closed *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x14000379020), Addr:(*net.TCPAddr)(0x14000379050), Err:poll.errNetClosing{}}
		// net_test.go:138: Error closing closed *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x14000379020), Addr:(*net.TCPAddr)(0x14000379050), Err:poll.errNetClosing{}}
		// net_test.go:138: Error closing closed *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x14000379020), Addr:(*net.TCPAddr)(0x14000379050), Err:poll.errNetClosing{}}
		// net_test.go:138: Error closing closed *net.TCPConn:
		//             *net.OpError: &net.OpError{Op:"close", Net:"tcp", Source:(*net.TCPAddr)(0x14000379020), Addr:(*net.TCPAddr)(0x14000379050), Err:poll.errNetClosing{}}
		t.Logf("Error closing closed %T:\n\t%T: %#v", conn, err, err)
	}
}

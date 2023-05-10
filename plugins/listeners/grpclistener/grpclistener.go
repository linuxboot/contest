package grpclistener

import (
	context "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	grpcreflect "github.com/bufbuild/connect-grpcreflect-go"
	"github.com/linuxboot/contest/pkg/api"
	"github.com/linuxboot/contest/pkg/buffer"
	"github.com/linuxboot/contest/pkg/job"
	"github.com/linuxboot/contest/pkg/types"
	"github.com/linuxboot/contest/pkg/xcontext"
	contestlistener "github.com/linuxboot/contest/plugins/listeners/grpclistener/gen/contest/v1"
	"github.com/linuxboot/contest/plugins/listeners/grpclistener/gen/contest/v1/contestlistenerconnect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/bufbuild/connect-go"
)

type Endpoint struct {
	buffer io.Reader
}

type GRPCListener struct {
	listenAddr string
}

type GRPCServer struct {
	ctx       xcontext.Context
	api       *api.API
	Endpoints map[int]*Endpoint
}

var waitForUpdate = 5 * time.Second

func New(listenAddr string) *GRPCListener {
	return &GRPCListener{listenAddr: listenAddr}
}

func (grpcl *GRPCListener) Serve(ctx xcontext.Context, a *api.API) error {
	ctx.Infof("Starting GRPCListener...\n")

	mux := http.NewServeMux()

	// Add reflection API
	reflector := grpcreflect.NewStaticReflector(
		"contest.v1.ConTestService",
	)

	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

	// Add our gRPC Service
	mux.Handle(contestlistenerconnect.NewConTestServiceHandler(&GRPCServer{
		ctx:       ctx,
		api:       a,
		Endpoints: make(map[int]*Endpoint),
	}))

	errCh := make(chan error, 1)
	// start the listener asynchronously, and report errors and completion via
	// channels.
	go func() {
		errCh <- http.ListenAndServe(
			":8080",
			h2c.NewHandler(mux, &http2.Server{}),
		)
	}()
	ctx.Infof("Started GRPC API listener on :8080")
	// wait for cancellation or for completion
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		ctx.Debugf("Received server shut down request")
		return nil
	}
}

func (s *GRPCServer) StartJob(ctx context.Context, req *connect.Request[contestlistener.StartJobRequest]) (*connect.Response[contestlistener.StartJobResponse], error) {
	w := buffer.New()
	s.ctx.AddWriter(w)

	if req.Msg.Job == nil {
		s.ctx.Errorf("Job is nil")
		return connect.NewResponse(&contestlistener.StartJobResponse{
			JobId: 0,
			Error: "Job is nil",
		}), fmt.Errorf("Job is nil")
	}

	if req.Msg.Requestor == "" {
		s.ctx.Errorf("Requestor is not set")

		return connect.NewResponse(&contestlistener.StartJobResponse{
			JobId: 0,
			Error: "Job is nil",
		}), fmt.Errorf("Requestor is not set")
	}

	resp, err := s.api.Start(s.ctx, api.EventRequestor(req.Msg.Requestor), string(req.Msg.Job))
	if err != nil {
		return connect.NewResponse(&contestlistener.StartJobResponse{
			JobId: 0,
			Error: "Job is nil",
		}), err
	}

	var r api.ResponseDataStart
	if api.ResponseTypeToName[resp.Type] == "ResponseTypeStart" {
		r = resp.Data.(api.ResponseDataStart)
	}

	// Add buffer to internal map
	s.Endpoints[int(r.JobID)] = &Endpoint{
		buffer: w,
	}

	return connect.NewResponse(&contestlistener.StartJobResponse{
		JobId: int32(r.JobID),
		Error: "",
	}), nil
}

func (s *GRPCServer) StatusJob(ctx context.Context, req *connect.Request[contestlistener.StatusJobRequest], stream *connect.ServerStream[contestlistener.StatusJobResponse]) error {
	if req.Msg.Requestor == "" {
		s.ctx.Errorf("Requestor is not set")

		return fmt.Errorf("Requestor is not set")
	}

	if s.Endpoints[int(req.Msg.JobId)] == nil {
		s.ctx.Errorf("JobID does not exist.")

		return fmt.Errorf("JobID does not exist.")
	}

	for {

		resp, err := s.getResponseFromAPI(req.Msg)
		if err != nil {
			s.ctx.Errorf("getResponseFromAPI: %w", err)

			return fmt.Errorf("getResponseFromAPI() = '%w'", err)
		}

		if resp.Status == nil {
			s.ctx.Errorf("api.Status(): Returned job.Status == nil")

			return fmt.Errorf("api.Status(): Returned job.Status == nil")
		}

		reportBytes, err := json.Marshal(resp.Status)
		if err != nil {
			s.ctx.Errorf("Unable to Marshal Status")

			return fmt.Errorf("Unable to Marshal Status")
		}

		if err := stream.Send(&contestlistener.StatusJobResponse{
			Status: resp.Status.State,
			Error:  resp.Status.StateErrMsg,
			Report: reportBytes,
		}); err != nil {
			return err
		}

		fmt.Printf("Job State: %s\n", resp.Status.State)

		// Job is not running anymore
		if resp.Status.State != string(job.EventJobStarted) {
			break
		}

		// Buffer was full - let's poll faster again.
		time.Sleep(waitForUpdate)
	}

	resp, err := s.getResponseFromAPI(req.Msg)
	if err != nil {
		s.ctx.Errorf("getResponseFromAPI: %w", err)

		return fmt.Errorf("getResponseFromAPI() = '%w'", err)
	}

	if resp.Status == nil {
		s.ctx.Errorf("api.Status(): Returned job.Status == nil")

		return fmt.Errorf("api.Status(): Returned job.Status == nil")
	}

	reportBytes, err := json.Marshal(resp.Status)
	if err != nil {
		s.ctx.Errorf("Unable to Marshal Status")

		return fmt.Errorf("Unable to Marshal Status")
	}

	if err := stream.Send(&contestlistener.StatusJobResponse{
		Status: resp.Status.State,
		Error:  resp.Status.StateErrMsg,
		Report: reportBytes,
	}); err != nil {
		return err
	}

	fmt.Printf("Last Job State: %s\n", resp.Status.State)

	return nil
}

func (s *GRPCServer) getResponseFromAPI(msg *contestlistener.StatusJobRequest) (api.ResponseDataStatus, error) {
	apiResp, err := s.api.Status(s.ctx, api.EventRequestor(msg.Requestor), types.JobID(msg.JobId))
	if err != nil {
		s.ctx.Errorf("api.Status() = '%v'", err)

		return api.ResponseDataStatus{}, err
	}

	if api.ResponseTypeToName[apiResp.Type] == "ResponseTypeStatus" {
		return apiResp.Data.(api.ResponseDataStatus), nil
	}

	return api.ResponseDataStatus{}, fmt.Errorf("unknown Message")
}

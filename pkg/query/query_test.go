package query

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/google/pprof/profile"
	"github.com/parca-dev/parca/pkg/storage"
	"github.com/parca-dev/parca/proto/gen/go/profilestore"
	pb "github.com/parca-dev/parca/proto/gen/go/query"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func Test_QueryRange_EmptyStore(t *testing.T) {
	ctx := context.Background()
	db := storage.OpenDB()
	q := New(log.NewNopLogger(), db, nil)

	// Query last 5 minutes
	end := time.Now()
	start := end.Add(-5 * time.Minute)

	resp, err := q.QueryRange(ctx, &pb.QueryRangeRequest{
		Query: "allocs",
		Start: timestamppb.New(start),
		End:   timestamppb.New(end),
		Limit: 10,
	})
	require.NoError(t, err)
	require.Empty(t, resp.Series)
}

func Test_QueryRange_Valid(t *testing.T) {
	ctx := context.Background()
	db := storage.OpenDB()
	s := storage.NewInMemoryProfileMetaStore()
	q := New(log.NewNopLogger(), db, s)

	app, err := db.Appender(ctx, labels.Labels{
		labels.Label{
			Name:  "__name__",
			Value: "allocs",
		},
	})
	require.NoError(t, err)

	f, err := os.Open("testdata/alloc_objects.pb.gz")
	require.NoError(t, err)
	p, err := profile.Parse(f)
	require.NoError(t, err)

	// Overwrite the profile's timestamp to be within the last 5min.
	p.TimeNanos = time.Now().UnixNano()

	err = app.Append(storage.ProfileFromPprof(s, p, 0))
	require.NoError(t, err)

	// Query last 5 minutes
	end := time.Now()
	start := end.Add(-5 * time.Minute)

	resp, err := q.QueryRange(ctx, &pb.QueryRangeRequest{
		Query: "allocs",
		Start: timestamppb.New(start),
		End:   timestamppb.New(end),
		Limit: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Series)
	require.Equal(t, 1, len(resp.Series))
	require.Equal(t, 1, len(resp.Series[0].Samples))
	require.Equal(t, &profilestore.LabelSet{
		Labels: []*profilestore.Label{
			{
				Name:  "__name__",
				Value: "allocs",
			},
		},
	}, resp.Series[0].Labelset)
	require.Equal(t, int64(310797348), resp.Series[0].Samples[0].Value)
}

func Test_QueryRange_Limited(t *testing.T) {
	ctx := context.Background()
	db := storage.OpenDB()
	s := storage.NewInMemoryProfileMetaStore()
	q := New(log.NewNopLogger(), db, s)

	f, err := os.Open("testdata/alloc_objects.pb.gz")
	require.NoError(t, err)
	p, err := profile.Parse(f)
	require.NoError(t, err)

	numSeries := 10
	for i := 0; i < numSeries; i++ {
		app, err := db.Appender(ctx, labels.Labels{
			labels.Label{
				Name:  "__name__",
				Value: "allocs",
			},
			labels.Label{
				Name:  "meta",
				Value: fmt.Sprintf("series_%v", i),
			},
		})
		require.NoError(t, err)

		// Overwrite the profile's timestamp to be within the last 5min.
		p.TimeNanos = time.Now().UnixNano()

		err = app.Append(storage.ProfileFromPprof(s, p, 0))
		require.NoError(t, err)
	}

	// Query last 5 minutes
	end := time.Now()
	start := end.Add(-5 * time.Minute)

	limit := rand.Intn(numSeries)
	resp, err := q.QueryRange(ctx, &pb.QueryRangeRequest{
		Query: "allocs",
		Start: timestamppb.New(start),
		End:   timestamppb.New(end),
		Limit: uint32(limit),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Series)
	require.Equal(t, limit, len(resp.Series))
	for i := 0; i < limit; i++ {
		require.Equal(t, 1, len(resp.Series[i].Samples))
	}
}

func Test_QueryRange_InputValidation(t *testing.T) {
	ctx := context.Background()
	end := time.Now()
	start := end.Add(-5 * time.Minute)

	tests := map[string]struct {
		req *pb.QueryRangeRequest
	}{
		"Empty query": {
			req: &pb.QueryRangeRequest{
				Query: "",
				Start: timestamppb.New(start),
				End:   timestamppb.New(end),
			},
		},
		"Empty start": {
			req: &pb.QueryRangeRequest{
				Query: "allocs",
				Start: nil,
				End:   timestamppb.New(end),
			},
		},
		"Empty End": {
			req: &pb.QueryRangeRequest{
				Query: "allocs",
				Start: timestamppb.New(start),
				End:   nil,
			},
		},
		"End before start": {
			req: &pb.QueryRangeRequest{
				Query: "allocs",
				Start: timestamppb.New(end),
				End:   timestamppb.New(start),
			},
		},
	}

	q := New(log.NewNopLogger(), nil, nil)

	t.Parallel()
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resp, err := q.QueryRange(ctx, test.req)
			require.Error(t, err)
			require.Empty(t, resp)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func Test_Query_InputValidation(t *testing.T) {
	ctx := context.Background()

	invalidMode := pb.QueryRequest_Mode(1000)
	invalidReportType := pb.QueryRequest_ReportType(1000)

	tests := map[string]struct {
		req *pb.QueryRequest
	}{
		"Invalid mode": {
			req: &pb.QueryRequest{
				Mode:       invalidMode,
				Options:    &pb.QueryRequest_Single{Single: &pb.SingleProfile{}},
				ReportType: *pb.QueryRequest_FLAMEGRAPH.Enum(),
			},
		},
		"Invalid report type": {
			req: &pb.QueryRequest{
				Mode:       *pb.QueryRequest_SINGLE.Enum(),
				Options:    &pb.QueryRequest_Single{Single: &pb.SingleProfile{}},
				ReportType: invalidReportType,
			},
		},
		"option doesn't match mode": {
			req: &pb.QueryRequest{
				Mode:       *pb.QueryRequest_SINGLE.Enum(),
				Options:    &pb.QueryRequest_Merge{Merge: &pb.MergeProfile{}},
				ReportType: *pb.QueryRequest_FLAMEGRAPH.Enum(),
			},
		},
		"option not provided": {
			req: &pb.QueryRequest{
				Mode:       *pb.QueryRequest_SINGLE.Enum(),
				Options:    nil,
				ReportType: *pb.QueryRequest_FLAMEGRAPH.Enum(),
			},
		},
	}

	q := New(log.NewNopLogger(), nil, nil)

	t.Parallel()
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resp, err := q.Query(ctx, test.req)
			require.Error(t, err)
			require.Empty(t, resp)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func Test_Query_Simple(t *testing.T) {
	ctx := context.Background()
	db := storage.OpenDB()
	s := storage.NewInMemoryProfileMetaStore()
	q := New(log.NewNopLogger(), db, s)

	app, err := db.Appender(ctx, labels.Labels{
		labels.Label{
			Name:  "__name__",
			Value: "allocs",
		},
	})
	require.NoError(t, err)

	f, err := os.Open("../storage/testdata/profile1.pb.gz")
	require.NoError(t, err)
	p1, err := profile.Parse(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	t1 := (time.Now().UnixNano() / 1000000) * 1000000
	p1.TimeNanos = t1

	err = app.Append(storage.ProfileFromPprof(s, p1, 0))
	require.NoError(t, err)

	_, err = q.Query(ctx, &pb.QueryRequest{
		Mode: pb.QueryRequest_SINGLE,
		Options: &pb.QueryRequest_Single{
			Single: &pb.SingleProfile{
				Query: "allocs",
				Time:  timestamppb.New(time.Unix(0, t1)),
			},
		},
		ReportType: pb.QueryRequest_FLAMEGRAPH,
	})

	//out, err := proto.Marshal(resp)
	//require.NoError(t, err)
	//err = ioutil.WriteFile("../../ui/packages/shared/profile/src/testdata/fg-simple.pb", out, 0644)
	//require.NoError(t, err)
}

func Test_Query_Diff(t *testing.T) {
	ctx := context.Background()
	db := storage.OpenDB()
	s := storage.NewInMemoryProfileMetaStore()
	q := New(log.NewNopLogger(), db, s)

	app, err := db.Appender(ctx, labels.Labels{
		labels.Label{
			Name:  "__name__",
			Value: "allocs",
		},
	})
	require.NoError(t, err)

	f, err := os.Open("../storage/testdata/profile1.pb.gz")
	require.NoError(t, err)
	p1, err := profile.Parse(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open("../storage/testdata/profile2.pb.gz")
	require.NoError(t, err)
	p2, err := profile.Parse(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	t1 := (time.Now().UnixNano() / 1000000) * 1000000
	p1.TimeNanos = t1

	err = app.Append(storage.ProfileFromPprof(s, p1, 0))
	require.NoError(t, err)

	time.Sleep(time.Millisecond * 10)

	t2 := (time.Now().UnixNano() / 1000000) * 1000000
	p2.TimeNanos = t2

	err = app.Append(storage.ProfileFromPprof(s, p2, 0))
	require.NoError(t, err)

	_, err = q.Query(ctx, &pb.QueryRequest{
		Mode: pb.QueryRequest_DIFF,
		Options: &pb.QueryRequest_Diff{
			Diff: &pb.DiffProfile{
				A: &pb.ProfileDiffSelection{
					Mode: pb.ProfileDiffSelection_SINGLE,
					Options: &pb.ProfileDiffSelection_Single{
						Single: &pb.SingleProfile{
							Query: "allocs",
							Time:  timestamppb.New(time.Unix(0, t1)),
						},
					},
				},
				B: &pb.ProfileDiffSelection{
					Mode: pb.ProfileDiffSelection_SINGLE,
					Options: &pb.ProfileDiffSelection_Single{
						Single: &pb.SingleProfile{
							Query: "allocs",
							Time:  timestamppb.New(time.Unix(0, t2)),
						},
					},
				},
			},
		},
		ReportType: pb.QueryRequest_FLAMEGRAPH,
	})

	//	out, err := proto.Marshal(resp)
	//	require.NoError(t, err)
	//	err = ioutil.WriteFile("../../ui/packages/shared/profile/src/testdata/fg-diff.pb", out, 0644)
	//	require.NoError(t, err)
}

/*
 *     Copyright 2020 The Dragonfly Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package peer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-http-utils/headers"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1 "d7y.io/api/pkg/apis/common/v1"

	"d7y.io/dragonfly/v2/client/daemon/test"
	"d7y.io/dragonfly/v2/client/util"
	logger "d7y.io/dragonfly/v2/internal/dflog"
	"d7y.io/dragonfly/v2/pkg/source"
	"d7y.io/dragonfly/v2/pkg/source/clients/httpprotocol"
)

func TestPieceDownloader_DownloadPiece(t *testing.T) {
	assert := testifyassert.New(t)
	source.UnRegister("http")
	require.Nil(t, source.Register("http", httpprotocol.NewHTTPSourceClient(), httpprotocol.Adapter))
	defer source.UnRegister("http")
	testData, err := os.ReadFile(test.File)
	assert.Nil(err, "load test file")
	pieceDownloadTimeout := 30 * time.Second

	tests := []struct {
		handleFunc      func(w http.ResponseWriter, r *http.Request)
		taskID          string
		pieceRange      string
		rangeStart      uint64
		rangeSize       uint32
		targetPieceData []byte
	}{
		{
			handleFunc: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal("/download/tas/task-0", r.URL.Path)
				data := []byte("test test ")
				w.Header().Set(headers.ContentLength, fmt.Sprintf("%d", len(data)))
				if _, err := w.Write(data); err != nil {
					t.Error(err)
				}
			},
			taskID:          "task-0",
			pieceRange:      "bytes=0-9",
			rangeStart:      0,
			rangeSize:       10,
			targetPieceData: []byte("test test "),
		},
		{
			handleFunc: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal("/download/tas/task-1", r.URL.Path)
				rg := util.MustParseRange(r.Header.Get("Range"), math.MaxInt64)
				w.Header().Set(headers.ContentLength, fmt.Sprintf("%d", rg.Length))
				if _, err := w.Write(testData[rg.Start : rg.Start+rg.Length]); err != nil {
					t.Error(err)
				}
			},
			taskID:          "task-1",
			pieceRange:      "bytes=0-99",
			rangeStart:      0,
			rangeSize:       100,
			targetPieceData: testData[:100],
		},
		{
			handleFunc: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal("/download/tas/task-2", r.URL.Path)
				rg := util.MustParseRange(r.Header.Get("Range"), math.MaxInt64)
				w.Header().Set(headers.ContentLength, fmt.Sprintf("%d", rg.Length))
				if _, err := w.Write(testData[rg.Start : rg.Start+rg.Length]); err != nil {
					t.Error(err)
				}
			},
			taskID:          "task-2",
			pieceRange:      fmt.Sprintf("bytes=512-%d", len(testData)-1),
			rangeStart:      512,
			rangeSize:       uint32(len(testData) - 512),
			targetPieceData: testData[512:],
		},
		{
			handleFunc: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal("/download/tas/task-3", r.URL.Path)
				rg := util.MustParseRange(r.Header.Get("Range"), math.MaxInt64)
				w.Header().Set(headers.ContentLength, fmt.Sprintf("%d", rg.Length))
				if _, err := w.Write(testData[rg.Start : rg.Start+rg.Length]); err != nil {
					t.Error(err)
				}
			},
			taskID:          "task-3",
			pieceRange:      "bytes=512-1024",
			rangeStart:      512,
			rangeSize:       513,
			targetPieceData: testData[512:1025],
		},
	}

	for _, tt := range tests {
		server := httptest.NewServer(http.HandlerFunc(tt.handleFunc))
		addr, _ := url.Parse(server.URL)
		pd := NewPieceDownloader(pieceDownloadTimeout, nil)
		hash := md5.New()
		hash.Write(tt.targetPieceData)
		digest := hex.EncodeToString(hash.Sum(nil)[:16])
		r, c, err := pd.DownloadPiece(context.Background(), &DownloadPieceRequest{
			TaskID:     tt.taskID,
			DstPid:     "",
			DstAddr:    addr.Host,
			CalcDigest: true,
			piece: &commonv1.PieceInfo{
				PieceNum:    0,
				RangeStart:  tt.rangeStart,
				RangeSize:   tt.rangeSize,
				PieceMd5:    digest,
				PieceOffset: tt.rangeStart,
				PieceStyle:  commonv1.PieceStyle_PLAIN,
			},
			log: logger.With("test", "test"),
		})
		assert.Nil(err, "downloaded piece should success")

		data, err := io.ReadAll(r)
		assert.Nil(err, "read piece data should success")
		c.Close()

		assert.Equal(data, tt.targetPieceData, "downloaded piece data should match")
		server.Close()
	}
}

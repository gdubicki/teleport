/*
 * Teleport
 * Copyright (C) 2024  Gravitational, Inc.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package presencev1_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"

	presencev1pb "github.com/gravitational/teleport/api/gen/proto/go/teleport/presence/v1"
	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/api/utils"
	"github.com/gravitational/teleport/lib/auth"
)

func newTestTLSServer(t testing.TB) *auth.TestTLSServer {
	as, err := auth.NewTestAuthServer(auth.TestAuthServerConfig{
		Dir:   t.TempDir(),
		Clock: clockwork.NewFakeClockAt(time.Now().Round(time.Second).UTC()),
	})
	require.NoError(t, err)

	srv, err := as.NewTestTLSServer()
	require.NoError(t, err)

	t.Cleanup(func() {
		err := srv.Close()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		require.NoError(t, err)
	})

	return srv
}

// TestGetRemoteCluster is an integration test that uses a real gRPC
// client/server.
func TestGetRemoteCluster(t *testing.T) {
	t.Parallel()
	srv := newTestTLSServer(t)
	ctx := context.Background()

	user, role, err := auth.CreateUserAndRole(
		srv.Auth(),
		"rc-getter",
		[]string{},
		[]types.Rule{
			{
				Resources: []string{types.KindRemoteCluster},
				Verbs:     []string{types.VerbRead},
			},
		})
	require.NoError(t, err)
	err = role.SetLabelMatchers(types.Allow, types.KindRemoteCluster, types.LabelMatchers{
		Labels: map[string]utils.Strings{
			"label": {"foo"},
		},
	})
	require.NoError(t, err)
	_, err = srv.Auth().UpsertRole(ctx, role)
	require.NoError(t, err)

	unprivilegedUser, _, err := auth.CreateUserAndRole(
		srv.Auth(),
		"no-perms",
		[]string{},
		[]types.Rule{},
	)
	require.NoError(t, err)

	matchingRC, err := types.NewRemoteCluster("matching")
	require.NoError(t, err)
	md := matchingRC.GetMetadata()
	md.Labels = map[string]string{"label": "foo"}
	matchingRC.SetMetadata(md)
	matchingRC, err = srv.Auth().CreateRemoteCluster(ctx, matchingRC)
	require.NoError(t, err)

	notMatchingRC, err := types.NewRemoteCluster("not-matching")
	require.NoError(t, err)
	md = notMatchingRC.GetMetadata()
	md.Labels = map[string]string{"label": "bar"}
	notMatchingRC.SetMetadata(md)
	notMatchingRC, err = srv.Auth().CreateRemoteCluster(ctx, notMatchingRC)
	require.NoError(t, err)

	tests := []struct {
		name        string
		user        string
		req         *presencev1pb.GetRemoteClusterRequest
		assertError require.ErrorAssertionFunc
		want        *types.RemoteClusterV3
	}{
		{
			name: "success",
			user: user.GetName(),
			req: &presencev1pb.GetRemoteClusterRequest{
				Name: matchingRC.GetName(),
			},
			assertError: require.NoError,
			want:        matchingRC.(*types.RemoteClusterV3),
		},
		{
			name: "no permissions",
			user: unprivilegedUser.GetName(),
			req: &presencev1pb.GetRemoteClusterRequest{
				Name: matchingRC.GetName(),
			},
			assertError: func(t require.TestingT, err error, i ...interface{}) {
				// Opaque no permission presents as not found
				require.True(t, trace.IsNotFound(err), "error should be not found")
			},
		},
		{
			name: "no permissions - unmatching rc",
			user: user.GetName(),
			req: &presencev1pb.GetRemoteClusterRequest{
				Name: notMatchingRC.GetName(),
			},
			assertError: func(t require.TestingT, err error, i ...interface{}) {
				// Opaque no permission presents as not found
				require.True(t, trace.IsNotFound(err), "error should be not found")
			},
		},
		{
			name: "validation - no name",
			user: user.GetName(),
			req: &presencev1pb.GetRemoteClusterRequest{
				Name: "",
			},
			assertError: func(t require.TestingT, err error, i ...interface{}) {
				require.ErrorContains(t, err, "must be specified")
				require.True(t, trace.IsBadParameter(err), "error should be bad parameter")
			},
		},
		{
			name: "doesnt exist",
			user: user.GetName(),
			req: &presencev1pb.GetRemoteClusterRequest{
				Name: "non-existent",
			},
			assertError: func(t require.TestingT, err error, i ...interface{}) {
				require.True(t, trace.IsNotFound(err), "error should be bad parameter")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := srv.NewClient(auth.TestUser(tt.user))
			require.NoError(t, err)

			bot, err := client.PresenceServiceClient().GetRemoteCluster(ctx, tt.req)
			tt.assertError(t, err)
			if tt.want != nil {
				// Check that the returned bot matches
				require.Empty(t, cmp.Diff(tt.want, bot, protocmp.Transform()))
			}
		})
	}
}

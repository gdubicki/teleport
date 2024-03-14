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

package testlib

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gravitational/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/integrations/access/common"
	"github.com/gravitational/teleport/integrations/access/msteams"
	"github.com/gravitational/teleport/integrations/access/msteams/msapi"
	"github.com/gravitational/teleport/integrations/lib"
	"github.com/gravitational/teleport/integrations/lib/logger"
	"github.com/gravitational/teleport/integrations/lib/testing/integration"
)

// MsTeamsSuite is the Slack access plugin test suite.
// It implements the testify.TestingSuite interface.
type MsTeamsSuite struct {
	*integration.AccessRequestSuite
	appConfig             *msteams.Config
	raceNumber            int
	fakeTeams             *FakeTeams
	fakeStatusSink        *integration.FakeStatusSink
	requester1TeamsUser   msapi.User
	requesterOSSTeamsUser msapi.User
	reviewer1TeamsUser    msapi.User
	reviewer2TeamsUser    msapi.User
}

// SetupTest starts a fake Slack, generates the plugin configuration, and loads
// the fixtures in Slack. It runs for each test.
func (s *MsTeamsSuite) SetupTest() {
	t := s.T()

	err := logger.Setup(logger.Config{Severity: "debug"})
	require.NoError(t, err)
	s.raceNumber = runtime.GOMAXPROCS(0)

	s.fakeTeams = NewFakeTeams(s.raceNumber)
	t.Cleanup(s.fakeTeams.Close)

	// We need requester users as well, the slack plugin sends messages to users
	// when their access request got approved.
	s.requesterOSSTeamsUser = s.fakeTeams.StoreUser(msapi.User{Name: "Requester OSS", Mail: integration.RequesterOSSUserName})
	s.requester1TeamsUser = s.fakeTeams.StoreUser(msapi.User{Name: "Requester Ent", Mail: integration.Requester1UserName})
	s.reviewer1TeamsUser = s.fakeTeams.StoreUser(msapi.User{Mail: integration.Reviewer1UserName})
	s.reviewer2TeamsUser = s.fakeTeams.StoreUser(msapi.User{Mail: integration.Reviewer2UserName})

	s.fakeStatusSink = &integration.FakeStatusSink{}

	var conf msteams.Config
	conf.Teleport = s.TeleportConfig()
	conf.MSAPI = s.fakeTeams.Config
	conf.MSAPI.SetBaseURLs(s.fakeTeams.URL(), s.fakeTeams.URL(), s.fakeTeams.URL())

	s.appConfig = &conf
}

// startApp starts the Slack plugin, waits for it to become ready and returns.
func (s *MsTeamsSuite) startApp() {
	t := s.T()
	t.Helper()

	app, err := msteams.NewApp(*s.appConfig)
	require.NoError(t, err)
	s.RunAndWaitReady(t, app)
}

func (s *MsTeamsSuite) TestMessagePosting() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	s.startApp()

	request := s.CreateAccessRequest(ctx, integration.RequesterOSSUserName, []string{s.reviewer1TeamsUser.Mail, s.reviewer2TeamsUser.Mail})

	pluginData := s.checkPluginData(ctx, request.GetName(), func(data msteams.PluginData) bool {
		return len(data.TeamsData) > 0
	})
	require.Len(t, pluginData.TeamsData, 2)

	title := "Access Request " + request.GetName()

	msgs, err := s.getNewMessages(ctx, 2)
	require.NoError(t, err)

	require.Equal(t, gjson.Get(msgs[0].Body, "attachments.0.content.body.0.text").String(), title)
	require.Equal(t, msgs[0].RecipientID, s.reviewer1TeamsUser.ID)

	require.Equal(t, gjson.Get(msgs[1].Body, "attachments.0.content.body.0.text").String(), title)
	require.Equal(t, msgs[1].RecipientID, s.reviewer2TeamsUser.ID)
}

func (s *MsTeamsSuite) TestRecipientsConfig() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	s.appConfig.Recipients = common.RawRecipientsMap{
		types.Wildcard: []string{s.reviewer2TeamsUser.Mail, s.reviewer1TeamsUser.ID},
	}

	s.startApp()

	request := s.CreateAccessRequest(ctx, integration.RequesterOSSUserName, nil)
	pluginData := s.checkPluginData(ctx, request.GetName(), func(data msteams.PluginData) bool {
		return len(data.TeamsData) > 0
	})
	require.Len(t, pluginData.TeamsData, 2)

	title := "Access Request " + request.GetName()

	msgs, err := s.getNewMessages(ctx, 2)
	require.NoError(t, err)

	require.Equal(t, gjson.Get(msgs[0].Body, "attachments.0.content.body.0.text").String(), title)
	require.Equal(t, msgs[0].RecipientID, s.reviewer1TeamsUser.ID)

	require.Equal(t, gjson.Get(msgs[1].Body, "attachments.0.content.body.0.text").String(), title)
	require.Equal(t, msgs[1].RecipientID, s.reviewer2TeamsUser.ID)
}

func (s *MsTeamsSuite) TestApproval() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	s.startApp()

	req := s.CreateAccessRequest(ctx, integration.RequesterOSSUserName, []string{s.reviewer1TeamsUser.Mail})
	msg, err := s.fakeTeams.CheckNewMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)

	err = s.Ruler().ApproveAccessRequest(ctx, req.GetName(), "okay")
	require.NoError(t, err)

	msgUpdate, err := s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)
	require.Equal(t, msg.ID, msgUpdate.ID)

	require.NoError(t, err)
	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.1.columns.0.items.0.text").String(), "✅")
	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.1.columns.1.items.0.text").String(), "APPROVED")
	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.2.facts.4.value").String(), "okay")
}

func (s *MsTeamsSuite) TestDenial() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	s.startApp()

	req := s.CreateAccessRequest(ctx, integration.RequesterOSSUserName, []string{s.reviewer1TeamsUser.Mail})
	msg, err := s.fakeTeams.CheckNewMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)

	err = s.Ruler().DenyAccessRequest(ctx, req.GetName(), "not okay")
	require.NoError(t, err)

	msgUpdate, err := s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)
	require.Equal(t, msg.ID, msgUpdate.ID)

	require.NoError(t, err)
	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.1.columns.0.items.0.text").String(), "❌")
	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.1.columns.1.items.0.text").String(), "DENIED")
	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.2.facts.4.value").String(), "not okay")
}

func (s *MsTeamsSuite) TestReviewReplies() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	if !s.TeleportFeatures().AdvancedAccessWorkflows {
		t.Skip("Doesn't work in OSS version")
	}

	s.startApp()

	req := s.CreateAccessRequest(ctx, integration.Requester1UserName, []string{s.reviewer1TeamsUser.Mail})
	s.checkPluginData(ctx, req.GetName(), func(data msteams.PluginData) bool {
		return len(data.TeamsData) > 0
	})

	msg, err := s.fakeTeams.CheckNewMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)

	err = s.Reviewer1().SubmitAccessRequestReview(ctx, req.GetName(), types.AccessReview{
		Author:        integration.Reviewer1UserName,
		ProposedState: types.RequestState_APPROVED,
		Created:       time.Now(),
		Reason:        "okay",
	})
	require.NoError(t, err)

	reply, err := s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, msg.RecipientID, reply.RecipientID)
	require.Equal(t, msg.ID, reply.ID)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.0.value").String(), "✅")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.1.value").String(), integration.Reviewer1UserName)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.3.value").String(), "okay")

	err = s.Reviewer2().SubmitAccessRequestReview(ctx, req.GetName(), types.AccessReview{
		Author:        integration.Reviewer2UserName,
		ProposedState: types.RequestState_DENIED,
		Created:       time.Now(),
		Reason:        "not okay",
	})
	require.NoError(t, err)

	reply, err = s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, msg.RecipientID, reply.RecipientID)
	require.Equal(t, msg.ID, reply.ID)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.0.value").String(), "❌")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.1.value").String(), integration.Reviewer2UserName)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.3.value").String(), "not okay")
}

func (s *MsTeamsSuite) TestApprovalByReview() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	if !s.TeleportFeatures().AdvancedAccessWorkflows {
		t.Skip("Doesn't work in OSS version")
	}

	s.startApp()

	req := s.CreateAccessRequest(ctx, integration.Requester1UserName, []string{s.reviewer1TeamsUser.Mail})
	s.checkPluginData(ctx, req.GetName(), func(data msteams.PluginData) bool {
		return len(data.TeamsData) > 0
	})

	msg, err := s.fakeTeams.CheckNewMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)

	err = s.Reviewer1().SubmitAccessRequestReview(ctx, req.GetName(), types.AccessReview{
		Author:        integration.Reviewer1UserName,
		ProposedState: types.RequestState_APPROVED,
		Created:       time.Now(),
		Reason:        "okay",
	})
	require.NoError(t, err)

	reply, err := s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, msg.RecipientID, reply.RecipientID)
	require.Equal(t, msg.ID, reply.ID)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.0.value").String(), "✅")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.1.value").String(), integration.Reviewer1UserName)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.3.value").String(), "okay")

	err = s.Reviewer2().SubmitAccessRequestReview(ctx, req.GetName(), types.AccessReview{
		Author:        integration.Reviewer2UserName,
		ProposedState: types.RequestState_APPROVED,
		Created:       time.Now(),
		Reason:        "finally okay",
	})
	require.NoError(t, err)

	reply, err = s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, msg.RecipientID, reply.RecipientID)
	require.Equal(t, msg.ID, reply.ID)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.0.value").String(), "✅")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.1.value").String(), integration.Reviewer2UserName)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.3.value").String(), "finally okay")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.1.columns.0.items.0.text").String(), "✅")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.1.columns.1.items.0.text").String(), "APPROVED")
}

func (s *MsTeamsSuite) TestDenialByReview() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	if !s.TeleportFeatures().AdvancedAccessWorkflows {
		t.Skip("Doesn't work in OSS version")
	}

	s.startApp()

	req := s.CreateAccessRequest(ctx, integration.Requester1UserName, []string{s.reviewer1TeamsUser.Mail})
	s.checkPluginData(ctx, req.GetName(), func(data msteams.PluginData) bool {
		return len(data.TeamsData) > 0
	})

	msg, err := s.fakeTeams.CheckNewMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)

	err = s.Reviewer1().SubmitAccessRequestReview(ctx, req.GetName(), types.AccessReview{
		Author:        integration.Reviewer1UserName,
		ProposedState: types.RequestState_DENIED,
		Created:       time.Now(),
		Reason:        "not okay",
	})
	require.NoError(t, err)

	reply, err := s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, msg.RecipientID, reply.RecipientID)
	require.Equal(t, msg.ID, reply.ID)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.0.value").String(), "❌")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.1.value").String(), integration.Reviewer1UserName)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.4.facts.3.value").String(), "not okay")

	err = s.Reviewer2().SubmitAccessRequestReview(ctx, req.GetName(), types.AccessReview{
		Author:        integration.Reviewer2UserName,
		ProposedState: types.RequestState_DENIED,
		Created:       time.Now(),
		Reason:        "finally not okay",
	})
	require.NoError(t, err)

	reply, err = s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)

	require.Equal(t, msg.RecipientID, reply.RecipientID)
	require.Equal(t, msg.ID, reply.ID)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.0.value").String(), "❌")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.1.value").String(), integration.Reviewer2UserName)
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.5.facts.3.value").String(), "finally not okay")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.1.columns.0.items.0.text").String(), "❌")
	require.Equal(t, gjson.Get(reply.Body, "attachments.0.content.body.1.columns.1.items.0.text").String(), "DENIED")
}

func (s *MsTeamsSuite) TestExpiration() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	s.startApp()

	request := s.CreateAccessRequest(ctx, integration.RequesterOSSUserName, []string{s.reviewer1TeamsUser.Mail})
	msg, err := s.fakeTeams.CheckNewMessage(ctx)
	require.NoError(t, err)
	require.Equal(t, s.reviewer1TeamsUser.ID, msg.RecipientID)

	s.checkPluginData(ctx, request.GetName(), func(data msteams.PluginData) bool {
		return len(data.TeamsData) > 0
	})

	err = s.Ruler().DeleteAccessRequest(ctx, request.GetName()) // simulate expiration
	require.NoError(t, err)

	msgUpdate, err := s.fakeTeams.CheckMessageUpdate(ctx)
	require.NoError(t, err)
	require.Equal(t, s.reviewer1TeamsUser.ID, msgUpdate.RecipientID)
	require.Equal(t, msg.ID, msgUpdate.ID)

	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.1.columns.0.items.0.text").String(), "⌛")
	require.Equal(t, gjson.Get(msgUpdate.Body, "attachments.0.content.body.1.columns.1.items.0.text").String(), "EXPIRED")
}
func (s *MsTeamsSuite) TestRace() {
	t := s.T()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	if !s.TeleportFeatures().AdvancedAccessWorkflows {
		t.Skip("Doesn't work in OSS version")
	}

	err := logger.Setup(logger.Config{Severity: "info"}) // Turn off noisy debug logging
	require.NoError(t, err)

	s.startApp()

	var (
		raceErr           error
		raceErrOnce       sync.Once
		msgIDs            sync.Map
		msgsCount         int32
		msgUpdateCounters sync.Map
	)
	setRaceErr := func(err error) error {
		raceErrOnce.Do(func() {
			raceErr = err
		})
		return err
	}

	process := lib.NewProcess(ctx)
	for i := 0; i < s.raceNumber; i++ {
		process.SpawnCritical(func(ctx context.Context) error {
			req, err := types.NewAccessRequest(uuid.New().String(), integration.Requester1UserName, "editor")
			if err != nil {
				return setRaceErr(trace.Wrap(err))
			}
			req.SetSuggestedReviewers([]string{s.reviewer1TeamsUser.Mail, s.reviewer2TeamsUser.Mail})
			if _, err := s.Requester1().CreateAccessRequestV2(ctx, req); err != nil {
				return setRaceErr(trace.Wrap(err))
			}
			return nil
		})
	}

	// Having TWO suggested reviewers will post TWO messages for each request.
	// We also have approval threshold of TWO set in the role properties
	// so lets simply submit the approval from each of the suggested reviewers.
	//
	// Multiplier SIX means that we handle TWO messages for each request and also
	// TWO comments for each message: 2 * (1 message + 2 comments).
	for i := 0; i < 2*s.raceNumber; i++ {
		process.SpawnCritical(func(ctx context.Context) error {
			msg, err := s.fakeTeams.CheckNewMessage(ctx)
			if err != nil {
				return setRaceErr(trace.Wrap(err))
			}

			threadMsgKey := msteams.TeamsMessage{ID: msg.ID, RecipientID: msg.RecipientID}
			if _, loaded := msgIDs.LoadOrStore(threadMsgKey, struct{}{}); loaded {
				return setRaceErr(trace.Errorf("thread %v already stored", threadMsgKey))
			}
			atomic.AddInt32(&msgsCount, 1)

			user, ok := s.fakeTeams.GetUser(msg.RecipientID)
			if !ok {
				return setRaceErr(trace.Errorf("user %s is not found", msg.RecipientID))
			}

			title := gjson.Get(msg.Body, "attachments.0.content.body.0.text").String()
			reqID := title[strings.LastIndex(title, " ")+1:]

			if err = s.ClientByName(user.Mail).SubmitAccessRequestReview(ctx, reqID, types.AccessReview{
				Author:        user.Mail,
				ProposedState: types.RequestState_APPROVED,
				Created:       time.Now(),
				Reason:        "okay",
			}); err != nil {
				return setRaceErr(trace.Wrap(err))
			}

			return nil
		})
	}

	// Multiplier TWO means that we handle updates for each of the two messages posted to reviewers.
	for i := 0; i < 4*s.raceNumber; i++ {
		process.SpawnCritical(func(ctx context.Context) error {
			msg, err := s.fakeTeams.CheckMessageUpdate(ctx)
			if err != nil {
				return setRaceErr(trace.Wrap(err))
			}

			threadMsgKey := msteams.TeamsMessage{ID: msg.ID, RecipientID: msg.RecipientID}
			var newCounter int32
			val, _ := msgUpdateCounters.LoadOrStore(threadMsgKey, &newCounter)
			counterPtr := val.(*int32)
			atomic.AddInt32(counterPtr, 1)

			return nil
		})
	}

	time.Sleep(1 * time.Second)

	process.Terminate()
	<-process.Done()
	require.NoError(t, raceErr)

	require.Equal(t, int32(2*s.raceNumber), msgsCount)
	msgIDs.Range(func(key, value interface{}) bool {
		next := true

		val, loaded := msgUpdateCounters.LoadAndDelete(key)
		next = next && assert.True(t, loaded)
		counterPtr := val.(*int32)
		next = next && assert.Equal(t, int32(2), *counterPtr)

		return next
	})
}

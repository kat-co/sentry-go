package sentry

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/suite"
)

type HubSuite struct {
	suite.Suite
	client *FakeClient
	scope  *Scope
	hub    *Hub
}

type FakeClient struct {
	lastCall     string
	lastCallArgs []interface{}
}

func (c *FakeClient) AddBreadcrumb(breadcrumb *Breadcrumb, scope Scoper) {
	c.lastCall = "AddBreadcrumb"
	c.lastCallArgs = []interface{}{breadcrumb, scope}
}

func (c *FakeClient) CaptureMessage(message string, scope Scoper) {
	c.lastCall = "CaptureMessage"
	c.lastCallArgs = []interface{}{message, scope}
}

func (c *FakeClient) CaptureException(exception error, scope Scoper) {
	c.lastCall = "CaptureException"
	c.lastCallArgs = []interface{}{exception, scope}
}

func (c *FakeClient) CaptureEvent(event *Event, scope Scoper) {
	c.lastCall = "CaptureEvent"
	c.lastCallArgs = []interface{}{event, scope}
}

func TestHubSuite(t *testing.T) {
	suite.Run(t, new(HubSuite))
}

func (suite *HubSuite) SetupTest() {
	suite.client = &FakeClient{}
	suite.scope = &Scope{}
	suite.hub = NewHub(suite.client, suite.scope)
}

func (suite *HubSuite) TestNewHubPushLayerOnTopOfStack() {
	suite.Len(*suite.hub.stack, 1)
}

func (suite *HubSuite) TestNewHubLayerStoresClientAndScope() {
	suite.Equal(&Layer{client: suite.client, scope: suite.scope}, (*suite.hub.stack)[0])
}

func (suite *HubSuite) TestPushScopeAddsScopeOnTopOfStack() {
	suite.hub.PushScope()

	suite.Len(*suite.hub.stack, 2)
}

func (suite *HubSuite) TestPushScopeInheritsScopeData() {
	suite.scope.SetExtra("foo", "bar")
	suite.hub.PushScope()
	suite.scope.SetExtra("baz", "qux")

	suite.False((*suite.hub.stack)[0].scope == (*suite.hub.stack)[1].scope, "Scope shouldnt point to the same struct")
	suite.Equal(map[string]interface{}{"foo": "bar", "baz": "qux"}, (*suite.hub.stack)[0].scope.extra)
	suite.Equal(map[string]interface{}{"foo": "bar"}, (*suite.hub.stack)[1].scope.extra)
}

func (suite *HubSuite) TestPushScopeInheritsClient() {
	suite.hub.PushScope()

	suite.True((*suite.hub.stack)[0].client == (*suite.hub.stack)[1].client, "Client should be inherited")
}

func (suite *HubSuite) TestPopScopeRemovesLayerFromTheStack() {
	suite.hub.PushScope()
	suite.hub.PushScope()
	suite.hub.PopScope()

	suite.Len(*suite.hub.stack, 2)
}

func (suite *HubSuite) TestPopScopeCannotRemoveFromEmptyStack() {
	suite.Len(*suite.hub.stack, 1)
	suite.hub.PopScope()
	suite.Len(*suite.hub.stack, 0)
	suite.hub.PopScope()
	suite.Len(*suite.hub.stack, 0)
}

func (suite *HubSuite) TestBindClient() {
	suite.hub.PushScope()
	newClient := &Client{}
	suite.hub.BindClient(newClient)

	suite.False(
		(*suite.hub.stack)[0].client == (*suite.hub.stack)[1].client,
		"Two stack layers should have different clients bound",
	)
	suite.True((*suite.hub.stack)[0].client == suite.client, "Stack's parent layer should have old client bound")
	suite.True((*suite.hub.stack)[1].client == newClient, "Stack's top layer should have new client bound")
}

func (suite *HubSuite) TestWithScope() {
	suite.hub.WithScope(func(scope *Scope) {
		suite.Len(*suite.hub.stack, 2)
	})

	suite.Len(*suite.hub.stack, 1)
}

func (suite *HubSuite) TestWithScopeBindClient() {
	suite.hub.WithScope(func(scope *Scope) {
		newClient := &Client{}
		suite.hub.BindClient(newClient)
		suite.True(suite.hub.stackTop().client == newClient)
	})

	suite.True(suite.hub.stackTop().client == suite.client)
}

func (suite *HubSuite) TestWithScopeDirectChanges() {
	suite.hub.Scope().SetExtra("foo", "bar")

	suite.hub.WithScope(func(scope *Scope) {
		scope.SetExtra("foo", "baz")
		suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
	})

	suite.Equal(map[string]interface{}{"foo": "bar"}, suite.hub.stackTop().scope.extra)
}

func (suite *HubSuite) TestWithScopeChangesThroughConfigureScope() {
	suite.hub.Scope().SetExtra("foo", "bar")

	suite.hub.WithScope(func(scope *Scope) {
		suite.hub.ConfigureScope(func(scope *Scope) {
			scope.SetExtra("foo", "baz")
		})
		suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
	})

	suite.Equal(map[string]interface{}{"foo": "bar"}, suite.hub.stackTop().scope.extra)
}

func (suite *HubSuite) TestConfigureScope() {
	suite.hub.Scope().SetExtra("foo", "bar")

	suite.hub.ConfigureScope(func(scope *Scope) {
		scope.SetExtra("foo", "baz")
		suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
	})

	suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
}

func (suite *HubSuite) TestLastEventID() {
	uuid := uuid.New()
	hub := &Hub{lastEventID: uuid}
	suite.Equal(uuid, hub.LastEventID())
}

func (suite *HubSuite) TestAccessingEmptyStack() {
	hub := &Hub{}
	suite.Nil(hub.stackTop())
}

func (suite *HubSuite) TestAccessingScopeReturnsNilIfStackIsEmpty() {
	hub := &Hub{}
	suite.Nil(hub.Scope())
}

func (suite *HubSuite) TestAccessingClientReturnsNilIfStackIsEmpty() {
	hub := &Hub{}
	suite.Nil(hub.Client())
}

func (suite *HubSuite) TestInvokeClientExecutesCallbackWithClientAndScopePassed() {
	callback := func(client Clienter, scope *Scope) {
		suite.Equal(suite.client, client)
		suite.Equal(suite.scope, scope)
	}
	suite.hub.invokeClient(callback)
}

func (suite *HubSuite) TestInvokeClientFailsSilentlyWHenNoClientOrScopeAvailable() {
	hub := &Hub{}
	callback := func(_ Clienter, _ *Scope) {
		suite.Fail("callback shoudnt be executed")
	}
	suite.NotPanics(func() {
		hub.invokeClient(callback)
	})
}

func (suite *HubSuite) TestCaptureEventCallsTheSameMethodOnClient() {
	event := &Event{Message: "CaptureEvent"}

	suite.hub.CaptureEvent(event)

	suite.Equal("CaptureEvent", suite.client.lastCall)
	suite.Equal(event, suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}

func (suite *HubSuite) TestCaptureMessageCallsTheSameMethodOnClient() {
	suite.hub.CaptureMessage("foo")

	suite.Equal("CaptureMessage", suite.client.lastCall)
	suite.Equal("foo", suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}

func (suite *HubSuite) TestCaptureExceptionCallsTheSameMethodOnClient() {
	err := errors.New("error")

	suite.hub.CaptureException(err)

	suite.Equal("CaptureException", suite.client.lastCall)
	suite.Equal(err, suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}

func (suite *HubSuite) TestAddBreadcrumbCallsTheSameMethodOnClient() {
	breadcrumb := &Breadcrumb{Message: "Breadcrumb"}

	suite.hub.AddBreadcrumb(breadcrumb)

	suite.Equal("AddBreadcrumb", suite.client.lastCall)
	suite.Equal(breadcrumb, suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}
package jira

// import (
// 	"context"
// 	"testing"

// 	"github.com/andygrunwald/go-jira"
// 	"github.com/stretchr/testify/assert"
// 	"github.com/stretchr/testify/mock"
// )

// // MockJiraClient mocks the go-jira Client
// type MockJiraClient struct {
// 	mock.Mock
// }

// func (m *MockJiraClient) Issue() *jira.IssueService {
// 	args := m.Called()
// 	return args.Get(0).(*jira.IssueService)
// }

// func (m *MockJiraClient) User() *jira.UserService {
// 	args := m.Called()
// 	return args.Get(0).(*jira.UserService)
// }

// // MockIssueService mocks the Issue service
// type MockIssueService struct {
// 	mock.Mock
// }

// func (m *MockIssueService) SearchWithContext(ctx context.Context, jql string, options *jira.SearchOptions) ([]jira.Issue, *jira.Response, error) {
// 	args := m.Called(ctx, jql, options)
// 	return args.Get(0).([]jira.Issue), args.Get(1).(*jira.Response), args.Error(2)
// }

// func (m *MockIssueService) DoTransitionWithContext(ctx context.Context, issueKey string, transitionID string) (*jira.Response, error) {
// 	args := m.Called(ctx, issueKey, transitionID)
// 	return args.Get(0).(*jira.Response), args.Error(1)
// }

// // MockUserService mocks the User service
// type MockUserService struct {
// 	mock.Mock
// }

// func (m *MockUserService) GetSelfWithContext(ctx context.Context) (*jira.User, *jira.Response, error) {
// 	args := m.Called(ctx)
// 	return args.Get(0).(*jira.User), args.Get(1).(*jira.Response), args.Error(2)
// }

// func TestGetTickets_Success(t *testing.T) {
// 	// Create mock JIRA client
// 	mockJiraClient := new(MockJiraClient)
// 	mockIssueService := new(MockIssueService)
// 	mockUserService := new(MockUserService)
// 	mockJiraClient.On("Issue").Return(mockIssueService)
// 	mockJiraClient.On("User").Return(mockUserService)

// 	// Create sample JIRA issues
// 	sampleIssues := []jira.Issue{
// 		{
// 			ID:  "123",
// 			Key: "PROJ-1",
// 			Fields: &jira.IssueFields{
// 				Summary:     "Test Ticket 1",
// 				Description: "Test Description 1",
// 				Status: &jira.Status{
// 					Name: "To Do",
// 				},
// 				Priority: &jira.Priority{
// 					Name: "High",
// 				},
// 				Assignee: &jira.User{
// 					DisplayName: "John Doe",
// 					Name:        "john.doe",
// 				},
// 				Reporter: &jira.User{
// 					DisplayName: "Jane Smith",
// 					Name:        "jane.smith",
// 				},
// 			},
// 			Self: "https://company.atlassian.net/rest/api/2/issue/123",
// 		},
// 	}

// 	// Setup mock to return sample issues
// 	mockIssueService.On("SearchWithContext", mock.Anything, mock.Anything, mock.Anything).Return(sampleIssues, &jira.Response{}, nil)

// 	// Create client with mock
// 	c := &client{jiraClient: mockJiraClient}

// 	// Test GetTickets
// 	tickets, err := c.GetTickets(context.Background(), "john.doe", "PROJ")

// 	// Assertions
// 	assert.NoError(t, err)
// 	assert.Len(t, tickets, 1)
// 	assert.Equal(t, "PROJ-1", tickets[0].Key)
// 	assert.Equal(t, "Test Ticket 1", tickets[0].Summary)
// 	assert.Equal(t, "To Do", tickets[0].Status)
// 	assert.Equal(t, "High", tickets[0].Priority)
// 	assert.Equal(t, "John Doe", tickets[0].Assignee)
// 	assert.Equal(t, "Jane Smith", tickets[0].Reporter)

// 	// Verify JQL construction
// 	mockIssueService.AssertCalled(t, "SearchWithContext", mock.Anything, "assignee = 'john.doe' AND project = 'PROJ' AND statusCategory != Done ORDER BY priority ASC", mock.Anything)
// }

// func TestGetTickets_Error(t *testing.T) {
// 	// Create mock JIRA client
// 	mockJiraClient := &MockJiraClient{}
// 	mockIssueService := &MockIssueService{}
// 	mockUserService := &MockUserService{}

// 	// Setup expectations
// 	mockJiraClient.On("Issue").Return(mockIssueService)
// 	mockJiraClient.On("User").Return(mockUserService)

// 	// Setup mock to return error
// 	mockIssueService.On("SearchWithContext", mock.Anything, mock.Anything, mock.Anything).Return([]jira.Issue{}, &jira.Response{}, assert.AnError)

// 	// Create client with mock
// 	c := &client{jiraClient: mockJiraClient}

// 	// Test GetTickets with error
// 	tickets, err := c.GetTickets(context.Background(), "john.doe", "PROJ")

// 	// Assertions
// 	assert.Error(t, err)
// 	assert.Nil(t, tickets)
// 	assert.Contains(t, err.Error(), "failed to fetch JIRA tickets")
// }

// func TestGetTickets_EmptyResponse(t *testing.T) {
// 	// Create mock JIRA client
// 	mockJiraClient := &MockJiraClient{}
// 	mockIssueService := &MockIssueService{}
// 	mockUserService := &MockUserService{}

// 	// Setup expectations
// 	mockJiraClient.On("Issue").Return(mockIssueService)
// 	mockJiraClient.On("User").Return(mockUserService)

// 	// Setup mock to return empty issues
// 	mockIssueService.On("SearchWithContext", mock.Anything, mock.Anything, mock.Anything).Return([]jira.Issue{}, &jira.Response{}, nil)

// 	// Create client with mock
// 	c := &client{jiraClient: mockJiraClient}

// 	// Test GetTickets with empty response
// 	tickets, err := c.GetTickets(context.Background(), "john.doe", "PROJ")

// 	// Assertions
// 	assert.NoError(t, err)
// 	assert.Len(t, tickets, 0)
// }

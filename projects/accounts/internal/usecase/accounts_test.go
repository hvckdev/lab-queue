package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"labqueue/projects/accounts/internal/model"
)

type accountService struct {
	calls []string
	err   error
	user  model.User
}

func (s *accountService) Register(context.Context, model.Registration) (model.User, error) {
	s.calls = append(s.calls, "register")
	return s.user, s.err
}
func (s *accountService) Authenticate(context.Context, model.Login) (model.User, error) {
	s.calls = append(s.calls, "authenticate")
	return s.user, s.err
}
func (s *accountService) Profile(context.Context, int64) (model.User, error) { return s.user, s.err }
func (s *accountService) StartSession(user model.User) (model.Authentication, error) {
	s.calls = append(s.calls, "session")
	return model.Authentication{User: user, Token: "signed"}, nil
}

func TestAuthenticationScenariosIssueTokenOnlyAfterSuccessfulAccountStep(t *testing.T) {
	for _, login := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			s := &accountService{user: model.User{ID: 7, Name: "Студент"}}
			if failed {
				s.err = errors.New("account step failed")
			}
			u := New(s, nil)
			var result model.Authentication
			var err error
			first := "register"
			if login {
				first = "authenticate"
				result, err = u.Login(context.Background(), model.Login{})
			} else {
				result, err = u.Register(context.Background(), model.Registration{})
			}
			want := []string{first}
			if !failed {
				want = append(want, "session")
				if err != nil || result.User.ID != 7 || result.Token != "signed" {
					t.Fatalf("result %+v, error %v", result, err)
				}
			} else if !errors.Is(err, s.err) {
				t.Fatalf("account error lost: %v", err)
			}
			if !reflect.DeepEqual(s.calls, want) {
				t.Fatalf("calls=%v want=%v", s.calls, want)
			}
		}
	}
}

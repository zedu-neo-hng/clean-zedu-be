package test_channel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/hngprojects/telex_be/external/request"
	"github.com/hngprojects/telex_be/internal/models"
	"github.com/hngprojects/telex_be/pkg/controller/auth"
	"github.com/hngprojects/telex_be/pkg/controller/channel"
	"github.com/hngprojects/telex_be/pkg/controller/organisation"
	"github.com/hngprojects/telex_be/pkg/middleware"
	"github.com/hngprojects/telex_be/pkg/repository/storage"
	tst "github.com/hngprojects/telex_be/tests"
	"github.com/hngprojects/telex_be/utility"
)

func TestFavouriteChannel(t *testing.T) {
	logger := tst.Setup()
	gin.SetMode(gin.TestMode)

	validatorRef := validator.New()
	db := storage.Connection()
	currUUID := utility.GenerateUUID()

	authCtrl := auth.Controller{
		Db: db, Validator: validatorRef, Logger: logger,
		ExtReq: request.ExternalRequest{Logger: logger, Test: true},
	}
	channelCtrl := channel.Controller{Db: db, Validator: validatorRef, Logger: logger}
	orgCtrl := organisation.Controller{Db: db, Validator: validatorRef, Logger: logger}

	r := gin.Default()

	channelUrl := r.Group("/api/v1/channels", middleware.Authorize(db.Postgresql))
	{
		channelUrl.POST("", channelCtrl.CreateChannel)
		channelUrl.POST("/:channelId/join", channelCtrl.JoinChannels)
		channelUrl.PUT("/:channelId/favourite", channelCtrl.SetFavouriteChannel)
	}

	orgUrl := r.Group("/api/v1/organisations", middleware.Authorize(db.Postgresql))
	{
		orgUrl.GET("/:org_id/user-channels", channelCtrl.GetUserChannels)
	}

	ownerSignUp := models.CreateUserRequestModel{
		Email:       fmt.Sprintf("fav_owner_%v@qa.team", currUUID),
		PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
		FirstName:   "Fav",
		LastName:    "Owner",
		Password:    "password",
		UserName:    fmt.Sprintf("fav_owner_%v", currUUID),
	}
	tst.SignupUser(t, r, authCtrl, ownerSignUp, false)
	ownerToken := tst.GetLoginToken(t, r, authCtrl, models.LoginRequestModel{
		Email:    ownerSignUp.Email,
		Password: ownerSignUp.Password,
	})

	orgId, _, _ := tst.CreateOrganisation(t, r, db, orgCtrl, models.CreateOrgRequestModel{
		Name:        fmt.Sprintf("OrgFavTest%s", currUUID),
		Description: "Org description",
		Email:       ownerSignUp.Email,
		Type:        "test",
		Location:    "test",
		Country:     "test",
	}, ownerToken)

	channelID, _ := tst.CreateChannels(t, r, channelCtrl, db, models.CreateChannelsRequest{
		Name:           fmt.Sprintf("fav_chan_%s", currUUID),
		Username:       fmt.Sprintf("fav_user_%s", currUUID),
		OrganisationID: orgId,
		Description:    "Channel used to test favourites",
	}, ownerToken)

	memberSignUp := models.CreateUserRequestModel{
		Email:       fmt.Sprintf("fav_member_%v@qa.team", currUUID),
		PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
		FirstName:   "Fav",
		LastName:    "Member",
		Password:    "password",
		UserName:    fmt.Sprintf("fav_member_%v", currUUID),
	}
	tst.SignupUser(t, r, authCtrl, memberSignUp, false)
	memberToken := tst.GetLoginToken(t, r, authCtrl, models.LoginRequestModel{
		Email:    memberSignUp.Email,
		Password: memberSignUp.Password,
	})

	var memberModel models.User
	db.Postgresql.Where("email = ?", memberSignUp.Email).First(&memberModel)
	db.Postgresql.Create(&models.OrgUserManagement{
		UserID:         memberModel.ID,
		OrganisationID: orgId,
		Status:         "active",
		RoleID:         utility.GenerateUUID(),
	})

	favouritePath := fmt.Sprintf("/api/v1/channels/%s/favourite", channelID)
	userChannelsPath := fmt.Sprintf("/api/v1/organisations/%s/user-channels", orgId)

	t.Run("channels are not favourites by default", func(t *testing.T) {
		if got := isFavouriteInList(t, r, userChannelsPath, ownerToken, channelID); got {
			t.Errorf("expected is_favourite=false before favouriting, got true")
		}
	})

	t.Run("favouriting is saved and returned in the channel list", func(t *testing.T) {
		rr := performPutRequest(t, r, favouritePath, ownerToken, map[string]bool{"is_favourite": true}, http.StatusOK)

		data, ok := tst.ParseResponse(rr)["data"].(map[string]any)
		if !ok || data["is_favourite"] != true {
			t.Fatalf("expected data.is_favourite=true in response, got %v", data)
		}

		if got := isFavouriteInList(t, r, userChannelsPath, ownerToken, channelID); !got {
			t.Errorf("expected is_favourite=true in user-channels after favouriting, got false")
		}
	})

	t.Run("favouriting twice keeps it favourited", func(t *testing.T) {
		performPutRequest(t, r, favouritePath, ownerToken, map[string]bool{"is_favourite": true}, http.StatusOK)

		if got := isFavouriteInList(t, r, userChannelsPath, ownerToken, channelID); !got {
			t.Errorf("expected is_favourite to stay true after a repeated request")
		}
	})

	t.Run("favourite is per user", func(t *testing.T) {
		performPostRequest(t, r, fmt.Sprintf("/api/v1/channels/%s/join", channelID), memberToken, map[string]any{}, http.StatusOK)

		if got := isFavouriteInList(t, r, userChannelsPath, memberToken, channelID); got {
			t.Errorf("expected another member to see is_favourite=false, got true")
		}
	})

	t.Run("unfavouriting is saved", func(t *testing.T) {
		rr := performPutRequest(t, r, favouritePath, ownerToken, map[string]bool{"is_favourite": false}, http.StatusOK)

		data, ok := tst.ParseResponse(rr)["data"].(map[string]any)
		if !ok || data["is_favourite"] != false {
			t.Fatalf("expected data.is_favourite=false in response, got %v", data)
		}

		if got := isFavouriteInList(t, r, userChannelsPath, ownerToken, channelID); got {
			t.Errorf("expected is_favourite=false after unfavouriting, got true")
		}
	})

	t.Run("missing is_favourite is rejected", func(t *testing.T) {
		performPutRequest(t, r, favouritePath, ownerToken, map[string]any{}, http.StatusUnprocessableEntity)
	})

	t.Run("invalid channel id is rejected", func(t *testing.T) {
		performPutRequest(t, r, "/api/v1/channels/not-a-uuid/favourite", ownerToken, map[string]bool{"is_favourite": true}, http.StatusBadRequest)
	})

	t.Run("non member cannot favourite a channel", func(t *testing.T) {
		outsiderSignUp := models.CreateUserRequestModel{
			Email:       fmt.Sprintf("fav_outsider_%v@qa.team", currUUID),
			PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
			FirstName:   "Fav",
			LastName:    "Outsider",
			Password:    "password",
			UserName:    fmt.Sprintf("fav_outsider_%v", currUUID),
		}
		tst.SignupUser(t, r, authCtrl, outsiderSignUp, false)
		outsiderToken := tst.GetLoginToken(t, r, authCtrl, models.LoginRequestModel{
			Email:    outsiderSignUp.Email,
			Password: outsiderSignUp.Password,
		})

		performPutRequest(t, r, favouritePath, outsiderToken, map[string]bool{"is_favourite": true}, http.StatusForbidden)
	})
}

func isFavouriteInList(t *testing.T, r *gin.Engine, path, token, channelID string) bool {
	t.Helper()

	rr := performGetRequest(t, r, path, token, http.StatusOK)
	list, ok := tst.ParseResponse(rr)["data"].([]any)
	if !ok {
		t.Fatalf("expected data to be a list of channels")
	}

	for _, item := range list {
		ch, ok := item.(map[string]any)
		if !ok || ch["channels_id"] != channelID {
			continue
		}
		fav, ok := ch["is_favourite"].(bool)
		if !ok {
			t.Fatalf("expected is_favourite to be present as a boolean on the channel")
		}
		return fav
	}

	t.Fatalf("channel %s not found in user-channels response", channelID)
	return false
}

func performPutRequest(t *testing.T, r *gin.Engine, path, token string, body any, expectedCode int) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(http.MethodPut, path, &buf)
	if err != nil {
		t.Fatalf("failed to create PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	tst.AssertStatusCode(t, rr.Code, expectedCode)
	return rr
}

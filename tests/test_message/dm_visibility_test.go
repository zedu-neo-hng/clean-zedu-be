package test_message

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hngprojects/telex_be/external/request"
	"github.com/hngprojects/telex_be/internal/models"
	"github.com/hngprojects/telex_be/pkg/controller/auth"
	dmCtrl "github.com/hngprojects/telex_be/pkg/controller/directMessage"
	"github.com/hngprojects/telex_be/pkg/middleware"
	"github.com/hngprojects/telex_be/pkg/repository/storage"
	tst "github.com/hngprojects/telex_be/tests"
	"github.com/hngprojects/telex_be/utility"
)

func TestDmChannelVisibilityFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := storage.Connection()
	logger := tst.Setup()
	validatorInstance := validator.New()
	currUUID := utility.GenerateUUID()

	defer tst.Cleanup(db)

	authController := auth.Controller{
		Db:        db,
		Validator: validatorInstance,
		Logger:    logger,
		ExtReq: request.ExternalRequest{
			Logger: logger,
			Test:   true,
		},
	}

	directMessageController := &dmCtrl.Controller{
		Db:        db,
		Validator: validatorInstance,
		Logger:    logger,
		ExtReq: request.ExternalRequest{
			Logger: logger,
			Test:   true,
		},
	}

	r := gin.Default()
	protected := r.Group("/api/v1", middleware.Authorize(db.Postgresql))
	{
		protected.GET("/organisations/:org_id/dms", directMessageController.GetDmChannels)
		protected.GET("/organisations/:org_id/dms/visible", directMessageController.GetVisibleDmChannels)
		protected.PATCH("/dms/:channel_id/visibility", directMessageController.UpdateDmVisibility)
	}

	user1SignUpData := models.CreateUserRequestModel{
		Email:       fmt.Sprintf("visuser1_%v@qa.team", currUUID),
		PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
		FirstName:   "VisUser",
		LastName:    "One",
		Password:    "password",
		UserName:    fmt.Sprintf("visuser1_%v", currUUID),
	}

	user2SignUpData := models.CreateUserRequestModel{
		Email:       fmt.Sprintf("visuser2_%v@qa.team", currUUID),
		PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
		FirstName:   "VisUser",
		LastName:    "Two",
		Password:    "password",
		UserName:    fmt.Sprintf("visuser2_%v", currUUID),
	}

	loginData1 := models.LoginRequestModel{
		Email:    user1SignUpData.Email,
		Password: user1SignUpData.Password,
	}

	loginData2 := models.LoginRequestModel{
		Email:    user2SignUpData.Email,
		Password: user2SignUpData.Password,
	}

	tst.SignupUser(t, r, authController, user1SignUpData, false)
	tst.SignupUser(t, gin.Default(), authController, user2SignUpData, false)
	token1 := tst.GetLoginToken(t, r, authController, loginData1)
	token2 := tst.GetLoginToken(t, r, authController, loginData2)

	var user1, user2 models.User
	require.NoError(t, db.Postgresql.Where("email = ?", user1SignUpData.Email).First(&user1).Error)
	require.NoError(t, db.Postgresql.Where("email = ?", user2SignUpData.Email).First(&user2).Error)

	var org models.Organisation
	require.NoError(t, db.Postgresql.Where("owner_id = ?", user1.ID).First(&org).Error)

	dmChannelID := utility.GenerateUUID()
	trueVal := true
	dmChan1 := models.DmChannels{
		ID:               utility.GenerateUUID(),
		ChannelId:        dmChannelID,
		UserId:           user1.ID,
		ParticipantId:    &user2.ID,
		OrgId:            org.ID,
		ChatType:         "user",
		ChannelType:      "dm",
		VisibilityStatus: &trueVal,
	}
	require.NoError(t, db.Postgresql.Create(&dmChan1).Error)

	dmChan2 := models.DmChannels{
		ID:               utility.GenerateUUID(),
		ChannelId:        dmChannelID,
		UserId:           user2.ID,
		ParticipantId:    &user1.ID,
		OrgId:            org.ID,
		ChatType:         "user",
		ChannelType:      "dm",
		VisibilityStatus: &trueVal,
	}
	require.NoError(t, db.Postgresql.Create(&dmChan2).Error)

	t.Run("Initially visible in GET /organisations/:org_id/dms/visible", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible", nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		data := response["data"].([]interface{})
		assert.NotEmpty(t, data)
	})

	t.Run("User 1 sets DM channel visibility to false", func(t *testing.T) {
		payload := map[string]bool{"visibility_status": false}
		jsonBody, _ := json.Marshal(payload)

		req := httptest.NewRequest("PATCH", "/api/v1/dms/"+dmChannelID+"/visibility", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, "DM channel visibility status updated successfully", response["message"].(string))
	})

	t.Run("Verify User 1 no longer sees channel in GET /dms/visible, but User 2 still sees it", func(t *testing.T) {
		// User 1 check
		req1 := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible", nil)
		req1.Header.Set("Authorization", "Bearer "+token1)
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)

		assert.Equal(t, http.StatusOK, w1.Code)
		var response1 map[string]interface{}
		require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &response1))
		data1 := response1["data"].([]interface{})
		for _, item := range data1 {
			ch := item.(map[string]interface{})
			assert.NotEqual(t, dmChannelID, ch["channel_id"])
		}

		// User 2 check
		req2 := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible", nil)
		req2.Header.Set("Authorization", "Bearer "+token2)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		assert.Equal(t, http.StatusOK, w2.Code)
		var response2 map[string]interface{}
		require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &response2))
		data2 := response2["data"].([]interface{})
		assert.NotEmpty(t, data2)
	})

	t.Run("Sending a new message in DM channel restores visibility_status to true for User 1", func(t *testing.T) {
		dmModel := models.DmChannels{ChannelId: dmChannelID}
		err := dmModel.UpdateInteractionAt(db.Postgresql)
		require.NoError(t, err)

		req1 := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible", nil)
		req1.Header.Set("Authorization", "Bearer "+token1)
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)

		assert.Equal(t, http.StatusOK, w1.Code)
		var response1 map[string]interface{}
		require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &response1))
		data1 := response1["data"].([]interface{})
		found := false
		for _, item := range data1 {
			ch := item.(map[string]interface{})
			if ch["channel_id"] == dmChannelID {
				found = true
				break
			}
		}
		assert.True(t, found, "DM channel should be restored to visible after new message")
	})

	t.Run("Channels are always ordered by recent activity without recent_dm param", func(t *testing.T) {
		dmChannelID2 := utility.GenerateUUID()
		pastTime := time.Now().Add(-24 * time.Hour)
		recentTime := time.Now()

		dmChanRecent := models.DmChannels{
			ID:               utility.GenerateUUID(),
			ChannelId:        dmChannelID2,
			UserId:           user1.ID,
			ParticipantId:    &user2.ID,
			OrgId:            org.ID,
			ChatType:         "user",
			ChannelType:      "dm",
			VisibilityStatus: &trueVal,
			CreatedAt:        pastTime,
			InteractedAt:     recentTime,
		}
		require.NoError(t, db.Postgresql.Create(&dmChanRecent).Error)

		thread := models.ThreadDocument{
			ID:         utility.GenerateUUID(),
			ChannelsID: dmChannelID2,
			Content:    "Hello recent message",
			Type:       "message",
			UserId:     user1.ID,
			CreatedAt:  recentTime,
		}
		require.NoError(t, thread.CreateThread(db, logger))

		req := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible", nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		data := response["data"].([]interface{})
		require.GreaterOrEqual(t, len(data), 2)

		firstChan := data[0].(map[string]interface{})
		assert.Equal(t, dmChannelID2, firstChan["channel_id"], "Channel with recent interaction should appear first")
	})

	t.Run("Page 2 preserves current_page and does not corrupt pagination metadata", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible?page=2&limit=1", nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		pagination := response["pagination"].(map[string]interface{})
		assert.Equal(t, float64(2), pagination["current_page"], "current_page should remain 2 on page 2")
	})
}

func TestDmVisibleChannelsFavouriteStamping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := storage.Connection()
	logger := tst.Setup()
	validatorInstance := validator.New()
	currUUID := utility.GenerateUUID()

	defer tst.Cleanup(db)

	authController := auth.Controller{
		Db:        db,
		Validator: validatorInstance,
		Logger:    logger,
		ExtReq: request.ExternalRequest{
			Logger: logger,
			Test:   true,
		},
	}

	directMessageController := &dmCtrl.Controller{
		Db:        db,
		Validator: validatorInstance,
		Logger:    logger,
		ExtReq: request.ExternalRequest{
			Logger: logger,
			Test:   true,
		},
	}

	r := gin.Default()
	protected := r.Group("/api/v1", middleware.Authorize(db.Postgresql))
	{
		protected.GET("/organisations/:org_id/dms/visible", directMessageController.GetVisibleDmChannels)
		protected.POST("/organisations/:org_id/dms/:channel_id/favourite", directMessageController.ToggleFavourite)
	}

	user1SignUpData := models.CreateUserRequestModel{
		Email:       fmt.Sprintf("fstamp_user1_%v@qa.team", currUUID),
		PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
		FirstName:   "Stamp",
		LastName:    "User1",
		Password:    "password",
		UserName:    fmt.Sprintf("fstamp_user1_%v", currUUID),
	}

	user2SignUpData := models.CreateUserRequestModel{
		Email:       fmt.Sprintf("fstamp_user2_%v@qa.team", currUUID),
		PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
		FirstName:   "Stamp",
		LastName:    "User2",
		Password:    "password",
		UserName:    fmt.Sprintf("fstamp_user2_%v", currUUID),
	}

	user3SignUpData := models.CreateUserRequestModel{
		Email:       fmt.Sprintf("fstamp_user3_%v@qa.team", currUUID),
		PhoneNumber: fmt.Sprintf("+234%v", utility.GetRandomNumbersInRange(7000000000, 9099999999)),
		FirstName:   "Stamp",
		LastName:    "User3",
		Password:    "password",
		UserName:    fmt.Sprintf("fstamp_user3_%v", currUUID),
	}

	loginData1 := models.LoginRequestModel{Email: user1SignUpData.Email, Password: user1SignUpData.Password}
	loginData2 := models.LoginRequestModel{Email: user2SignUpData.Email, Password: user2SignUpData.Password}
	loginData3 := models.LoginRequestModel{Email: user3SignUpData.Email, Password: user3SignUpData.Password}

	tst.SignupUser(t, r, authController, user1SignUpData, false)
	tst.SignupUser(t, gin.Default(), authController, user2SignUpData, false)
	tst.SignupUser(t, gin.Default(), authController, user3SignUpData, false)

	token1 := tst.GetLoginToken(t, r, authController, loginData1)
	token2 := tst.GetLoginToken(t, r, authController, loginData2)
	token3 := tst.GetLoginToken(t, r, authController, loginData3)

	var user1, user2, user3 models.User
	require.NoError(t, db.Postgresql.Where("email = ?", user1SignUpData.Email).First(&user1).Error)
	require.NoError(t, db.Postgresql.Where("email = ?", user2SignUpData.Email).First(&user2).Error)
	require.NoError(t, db.Postgresql.Where("email = ?", user3SignUpData.Email).First(&user3).Error)

	var org, org2 models.Organisation
	require.NoError(t, db.Postgresql.Where("owner_id = ?", user1.ID).First(&org).Error)
	require.NoError(t, db.Postgresql.Where("owner_id = ?", user3.ID).First(&org2).Error)

	trueVal := true

	// Regular DM channel owned by user1
	dmChannelID := utility.GenerateUUID()
	dmChan := models.DmChannels{
		ID:               utility.GenerateUUID(),
		ChannelId:        dmChannelID,
		UserId:           user1.ID,
		ParticipantId:    &user2.ID,
		OrgId:            org.ID,
		ChatType:         "user",
		ChannelType:      "dm",
		VisibilityStatus: &trueVal,
	}
	require.NoError(t, db.Postgresql.Create(&dmChan).Error)

	// Never-favourited DM channel owned by user1
	neverFavChannelID := utility.GenerateUUID()
	neverFavChan := models.DmChannels{
		ID:               utility.GenerateUUID(),
		ChannelId:        neverFavChannelID,
		UserId:           user1.ID,
		ParticipantId:    &user2.ID,
		OrgId:            org.ID,
		ChatType:         "user",
		ChannelType:      "dm",
		VisibilityStatus: &trueVal,
	}
	require.NoError(t, db.Postgresql.Create(&neverFavChan).Error)

	// Group DM channel owned by user1, with user1 and user2 as participants
	groupDMChannelID := utility.GenerateUUID()
	groupDMChan := models.DmChannels{
		ID:               utility.GenerateUUID(),
		UserId:           user1.ID,
		ChannelId:        groupDMChannelID,
		OrgId:            org.ID,
		ParticipantHash:  utility.GenerateUUID(),
		ChatType:         "user",
		ChannelType:      "group_dm",
		VisibilityStatus: &trueVal,
	}
	require.NoError(t, db.Postgresql.Create(&groupDMChan).Error)

	for _, user := range []models.User{user1, user2} {
		participant := models.ChannelParticipant{
			ID:        utility.GenerateUUID(),
			ChannelId: groupDMChannelID,
			UserId:    user.ID,
			OrgId:     org.ID,
		}
		require.NoError(t, db.Postgresql.Create(&participant).Error)
	}

	getIsFavourite := func(token, channelID string) bool {
		req := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		data := response["data"].([]interface{})

		for _, item := range data {
			ch := item.(map[string]interface{})
			if ch["channel_id"] == channelID {
				isFav, _ := ch["is_favourite"].(bool)
				return isFav
			}
		}
		return false
	}

	toggleFavourite := func(token, orgID, channelID string) {
		req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/organisations/%s/dms/%s/favourite", orgID, channelID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
	}

	t.Run("favourite -> is_favourite=true", func(t *testing.T) {
		toggleFavourite(token1, org.ID, dmChannelID)
		assert.True(t, getIsFavourite(token1, dmChannelID), "dmChannelID should be favourited")
	})

	t.Run("unfavourite -> is_favourite=false", func(t *testing.T) {
		toggleFavourite(token1, org.ID, dmChannelID)
		assert.False(t, getIsFavourite(token1, dmChannelID), "dmChannelID should be unfavourited")
	})

	t.Run("group DM via channel_participants join -> is_favourite=true", func(t *testing.T) {
		toggleFavourite(token1, org.ID, groupDMChannelID)
		assert.True(t, getIsFavourite(token1, groupDMChannelID), "groupDMChannelID should be favourited")
	})

	t.Run("never-favourited -> explicit is_favourite=false", func(t *testing.T) {
		assert.False(t, getIsFavourite(token1, neverFavChannelID), "neverFavChannelID should show is_favourite=false")
	})

	t.Run("no cross-user leakage", func(t *testing.T) {
		// User2 favourites the group DM; user1 should NOT see is_favourite=true for it
		toggleFavourite(token2, org.ID, groupDMChannelID)
		toggleFavourite(token1, org.ID, groupDMChannelID) // user1 unfavourites it
		assert.False(t, getIsFavourite(token1, groupDMChannelID), "user1 should not see user2's favourite")
	})

	t.Run("no cross-org leakage", func(t *testing.T) {
		// Create and favourite a channel in org2 as user3
		org2ChannelID := utility.GenerateUUID()
		org2Chan := models.DmChannels{
			ID:               utility.GenerateUUID(),
			ChannelId:        org2ChannelID,
			UserId:           user3.ID,
			ParticipantId:    &user3.ID,
			OrgId:            org2.ID,
			ChatType:         "user",
			ChannelType:      "dm",
			VisibilityStatus: &trueVal,
		}
		require.NoError(t, db.Postgresql.Create(&org2Chan).Error)

		toggleFavourite(token3, org2.ID, org2ChannelID)

		// User1 fetching visible DMs from org1 should not see org2's channel
		req := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible", nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		data := response["data"].([]interface{})

		for _, item := range data {
			ch := item.(map[string]interface{})
			assert.NotEqual(t, org2ChannelID, ch["channel_id"], "org2 channel should not leak into org1 visible DMs")
		}
	})

	t.Run("pagination integrity", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/organisations/"+org.ID+"/dms/visible?page=2&limit=1", nil)
		req.Header.Set("Authorization", "Bearer "+token1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		pagination := response["pagination"].(map[string]interface{})
		assert.Equal(t, float64(2), pagination["current_page"], "current_page should remain 2 on page 2")
	})
}


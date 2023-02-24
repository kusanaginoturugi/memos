package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"

	"github.com/pkg/errors"
	"github.com/usememos/memos/api"
	"github.com/usememos/memos/common"
	"golang.org/x/exp/slices"

	"github.com/labstack/echo/v4"
)

func (s *Server) registerTagRoutes(g *echo.Group) {
	g.POST("/tag", func(c echo.Context) error {
		ctx := c.Request().Context()
		user := c.Get(userContextKey).(*api.User)

		tagUpsert := &api.TagUpsert{}
		if err := json.NewDecoder(c.Request().Body).Decode(tagUpsert); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Malformed post tag request").SetInternal(err)
		}
		if tagUpsert.Name == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Tag name shouldn't be empty")
		}

		tagUpsert.CreatorID = user.ID
		tag, err := s.Store.UpsertTag(ctx, tagUpsert)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to upsert tag").SetInternal(err)
		}
		if err := s.createTagCreateActivity(c, tag); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create activity").SetInternal(err)
		}
		return c.JSON(http.StatusOK, composeResponse(tag.Name))
	}, loginOnlyMiddleware)

	g.GET("/tag", func(c echo.Context) error {
		ctx := c.Request().Context()
		user := c.Get(userContextKey).(*api.User)

		tagFind := &api.TagFind{
			CreatorID: user.ID,
		}
		tagList, err := s.Store.FindTagList(ctx, tagFind)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find tag list").SetInternal(err)
		}

		tagNameList := []string{}
		for _, tag := range tagList {
			tagNameList = append(tagNameList, tag.Name)
		}
		return c.JSON(http.StatusOK, composeResponse(tagNameList))
	}, loginOnlyMiddleware)

	g.GET("/tag/suggestion", func(c echo.Context) error {
		ctx := c.Request().Context()
		user := c.Get(userContextKey).(*api.User)

		contentSearch := "#"
		normalRowStatus := api.Normal
		memoFind := api.MemoFind{
			CreatorID:     &user.ID,
			ContentSearch: &contentSearch,
			RowStatus:     &normalRowStatus,
		}

		memoList, err := s.Store.FindMemoList(ctx, &memoFind)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find memo list").SetInternal(err)
		}

		tagFind := &api.TagFind{
			CreatorID: user.ID,
		}
		existTagList, err := s.Store.FindTagList(ctx, tagFind)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find tag list").SetInternal(err)
		}
		tagNameList := []string{}
		for _, tag := range existTagList {
			tagNameList = append(tagNameList, tag.Name)
		}

		tagMapSet := make(map[string]bool)
		for _, memo := range memoList {
			for _, tag := range findTagListFromMemoContent(memo.Content) {
				if !slices.Contains(tagNameList, tag) {
					tagMapSet[tag] = true
				}
			}
		}
		tagList := []string{}
		for tag := range tagMapSet {
			tagList = append(tagList, tag)
		}
		sort.Strings(tagList)
		return c.JSON(http.StatusOK, composeResponse(tagList))
	}, loginOnlyMiddleware)

	g.POST("/tag/delete", func(c echo.Context) error {
		ctx := c.Request().Context()
		user := c.Get(userContextKey).(*api.User)

		tagDelete := &api.TagDelete{}
		if err := json.NewDecoder(c.Request().Body).Decode(tagDelete); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Malformed post tag request").SetInternal(err)
		}
		if tagDelete.Name == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Tag name shouldn't be empty")
		}

		tagDelete.CreatorID = user.ID
		if err := s.Store.DeleteTag(ctx, tagDelete); err != nil {
			if common.ErrorCode(err) == common.NotFound {
				return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("Tag name not found: %s", tagDelete.Name))
			}
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to delete tag name: %v", tagDelete.Name)).SetInternal(err)
		}
		return c.JSON(http.StatusOK, true)
	}, loginOnlyMiddleware)
}

var tagRegexp = regexp.MustCompile(`#([^\s#]+)`)

func findTagListFromMemoContent(memoContent string) []string {
	tagMapSet := make(map[string]bool)
	matches := tagRegexp.FindAllStringSubmatch(memoContent, -1)
	for _, v := range matches {
		tagName := v[1]
		tagMapSet[tagName] = true
	}

	tagList := []string{}
	for tag := range tagMapSet {
		tagList = append(tagList, tag)
	}
	sort.Strings(tagList)
	return tagList
}

func (s *Server) createTagCreateActivity(c echo.Context, tag *api.Tag) error {
	ctx := c.Request().Context()
	payload := api.ActivityTagCreatePayload{
		TagName: tag.Name,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal activity payload")
	}
	activity, err := s.Store.CreateActivity(ctx, &api.ActivityCreate{
		CreatorID: tag.CreatorID,
		Type:      api.ActivityTagCreate,
		Level:     api.ActivityInfo,
		Payload:   string(payloadBytes),
	})
	if err != nil || activity == nil {
		return errors.Wrap(err, "failed to create activity")
	}
	return err
}

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileService_UpdateSuccess(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, auditRepo, txMgr := testRepos(db)
	tester := seedTester(t, db)
	svc := NewProfileService(userRepo, auditRepo, txMgr)

	name := "Tester Updated"
	phone := "13900009999"
	updated, err := svc.Update(context.Background(), tester, UpdateProfileInput{
		Name: &name, Phone: &phone,
	})
	require.NoError(t, err)
	assert.Equal(t, "Tester Updated", updated.Name)
}

func TestProfileService_UpdateEmailBlocked(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, auditRepo, txMgr := testRepos(db)
	tester := seedTester(t, db)
	svc := NewProfileService(userRepo, auditRepo, txMgr)

	email := "newemail@test.local"
	_, err := svc.Update(context.Background(), tester, UpdateProfileInput{
		Email: &email,
	})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 400, bizErr.Status)
}

func TestProfileService_UpdateAvatar(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, auditRepo, txMgr := testRepos(db)
	tester := seedTester(t, db)
	svc := NewProfileService(userRepo, auditRepo, txMgr)

	err := svc.UpdateAvatar(context.Background(), tester, "/uploads/avatars/test.png")
	require.NoError(t, err)
}

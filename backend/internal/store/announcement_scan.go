package store

import (
	"database/sql"

	"studyhub/internal/core"
	"studyhub/internal/models"
)

// AnnouncementColumns is THE select list for an announcement, read by ScanAnnouncements.
// The snapshot and /api/announcements kept separate copies, and only one learned the board fields (0049).
const AnnouncementColumns = `id,title,message,audience,type,created_on,created_by,status,archive_on,COALESCE(target_class_ids,''),COALESCE(category,'notice'),COALESCE(pinned,FALSE),COALESCE(pin_requested,FALSE),COALESCE(updated_on,'')`

// ScanAnnouncements reads rows selected with AnnouncementColumns; an unreadable row is logged and skipped.
func ScanAnnouncements(rows *sql.Rows) []models.Announcement {
	out := []models.Announcement{}
	for rows.Next() {
		var a models.Announcement
		var status, archiveOn sql.NullString
		var targets string
		if err := rows.Scan(&a.ID, &a.Title, &a.Message, &a.Audience, &a.Type, &a.CreatedOn, &a.CreatedBy, &status, &archiveOn, &targets, &a.Category, &a.Pinned, &a.PinRequested, &a.UpdatedOn); err != nil {
			core.Logger.Error("announcement row unreadable", "err", err)
			continue
		}
		a.TargetClassIDs = models.ParseArr(targets)
		a.Status = models.NullStr(status)
		if a.Status == "" {
			a.Status = "published"
		}
		a.ArchiveOn = models.NullStr(archiveOn)
		out = append(out, a)
	}
	return out
}

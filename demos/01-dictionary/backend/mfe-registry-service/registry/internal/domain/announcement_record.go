package domain

import "time"

// StampAnnouncement records observation time, never a publisher's claimed age.
// Pending rows have no TTL. Re-announcement retains the first
// observation; ignored announcements leave the entry unchanged and use only
// the audit row's timestamp instead (decision 77, gate answer 2).
func StampAnnouncement(existing *Entry, next Entry, now time.Time) Entry {
	next.AnnouncedAt = now.UTC().Format(time.RFC3339Nano)
	if existing != nil && existing.AnnouncedAt != "" {
		next.AnnouncedAt = existing.AnnouncedAt
	}
	next.LastAnnouncedAt = now.UTC().Format(time.RFC3339Nano)
	return next
}

const IgnoredAnnouncementDetail = "static entry takes precedence over announcement"

-- name: ListLeaderboardForLeague :many
-- Backs GET /leagues/:id/leaderboard. Every non-removed membership
-- (contestants and manage-only commissioners alike — the API contract
-- doesn't exclude non-contestants, it just documents their is_contestant
-- flag so clients can badge them differently). Sort: active first (a
-- non-contestant's status is always 'active' since they never play, so
-- they naturally land in this bucket), then eliminated members ordered by
-- how late they were eliminated (joined against weeks.week_number, since
-- eliminated_week_id is a UUID with no inherent order) — descending, so
-- "eliminated later" (survived longer) ranks higher. display_name is a
-- final stable tie-break.
SELECT
    m.id AS membership_id,
    u.display_name AS display_name,
    m.status AS status,
    m.is_contestant AS is_contestant,
    m.eliminated_week_id AS eliminated_week_id,
    m.bought_back AS bought_back
FROM league_memberships m
JOIN users u ON u.id = m.user_id
LEFT JOIN weeks w ON w.id = m.eliminated_week_id
WHERE m.league_id = sqlc.arg(league_id) AND m.removed_at IS NULL
ORDER BY
    (m.status <> 'active'),
    w.week_number DESC NULLS LAST,
    u.display_name ASC;

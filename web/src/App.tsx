import { Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { ProtectedRoute } from './components/ProtectedRoute'
import { SiteAdminRoute } from './components/SiteAdminRoute'
import { HomePage } from './routes/HomePage'
import { LoginPage } from './routes/LoginPage'
import { RegisterPage } from './routes/RegisterPage'
import { ForgotPasswordPage } from './routes/ForgotPasswordPage'
import { ResetPasswordPage } from './routes/ResetPasswordPage'
import { HealthCheckPage } from './routes/HealthCheckPage'
import { CreateLeaguePage } from './routes/CreateLeaguePage'
import { JoinLeaguePage } from './routes/JoinLeaguePage'
import { LeagueDetailPage } from './routes/LeagueDetailPage'
import { PicksPage } from './routes/PicksPage'
import { LeaderboardPage } from './routes/LeaderboardPage'
import { NotificationPreferencesPage } from './routes/NotificationPreferencesPage'
import { AdminOverviewPage } from './routes/AdminOverviewPage'
import { AdminLeaguesPage } from './routes/AdminLeaguesPage'
import { AdminUsersPage } from './routes/AdminUsersPage'
import { AdminSyncRunsPage } from './routes/AdminSyncRunsPage'
import { AdminResyncGamePage } from './routes/AdminResyncGamePage'
import { AdminAuditLogPage } from './routes/AdminAuditLogPage'
import { NotFoundPage } from './routes/NotFoundPage'

function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/forgot-password" element={<ForgotPasswordPage />} />
        <Route path="/reset-password" element={<ResetPasswordPage />} />
        <Route path="/health" element={<HealthCheckPage />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <HomePage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/leagues/new"
          element={
            <ProtectedRoute>
              <CreateLeaguePage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/leagues/join"
          element={
            <ProtectedRoute>
              <JoinLeaguePage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/leagues/:id"
          element={
            <ProtectedRoute>
              <LeagueDetailPage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/leagues/:id/picks"
          element={
            <ProtectedRoute>
              <PicksPage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/leagues/:id/leaderboard"
          element={
            <ProtectedRoute>
              <LeaderboardPage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/notification-preferences"
          element={
            <ProtectedRoute>
              <NotificationPreferencesPage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/admin"
          element={
            <SiteAdminRoute>
              <AdminOverviewPage />
            </SiteAdminRoute>
          }
        />
        <Route
          path="/admin/leagues"
          element={
            <SiteAdminRoute>
              <AdminLeaguesPage />
            </SiteAdminRoute>
          }
        />
        <Route
          path="/admin/users"
          element={
            <SiteAdminRoute>
              <AdminUsersPage />
            </SiteAdminRoute>
          }
        />
        <Route
          path="/admin/sync-runs"
          element={
            <SiteAdminRoute>
              <AdminSyncRunsPage />
            </SiteAdminRoute>
          }
        />
        <Route
          path="/admin/resync-game"
          element={
            <SiteAdminRoute>
              <AdminResyncGamePage />
            </SiteAdminRoute>
          }
        />
        <Route
          path="/admin/audit-log"
          element={
            <SiteAdminRoute>
              <AdminAuditLogPage />
            </SiteAdminRoute>
          }
        />
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </AuthProvider>
  )
}

export default App

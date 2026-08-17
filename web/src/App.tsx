import { Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { ProtectedRoute } from './components/ProtectedRoute'
import { HomePage } from './routes/HomePage'
import { LoginPage } from './routes/LoginPage'
import { RegisterPage } from './routes/RegisterPage'
import { HealthCheckPage } from './routes/HealthCheckPage'
import { CreateLeaguePage } from './routes/CreateLeaguePage'
import { JoinLeaguePage } from './routes/JoinLeaguePage'
import { LeagueDetailPage } from './routes/LeagueDetailPage'
import { PicksPage } from './routes/PicksPage'
import { LeaderboardPage } from './routes/LeaderboardPage'

function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
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
      </Routes>
    </AuthProvider>
  )
}

export default App

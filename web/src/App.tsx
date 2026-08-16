import { Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import { ProtectedRoute } from './components/ProtectedRoute'
import { HomePage } from './routes/HomePage'
import { LoginPage } from './routes/LoginPage'
import { RegisterPage } from './routes/RegisterPage'
import { HealthCheckPage } from './routes/HealthCheckPage'

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
      </Routes>
    </AuthProvider>
  )
}

export default App

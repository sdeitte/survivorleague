import { z } from 'zod'

// Mirrors the backend's validation rules exactly (see
// api/internal/httpapi/validate.go): a simple email-shape check and an
// 8-character password minimum.
export const loginSchema = z.object({
  email: z.string().trim().min(1, 'Email is required').email('Enter a valid email address'),
  password: z.string().min(1, 'Password is required'),
})
export type LoginFormValues = z.infer<typeof loginSchema>

export const registerSchema = z.object({
  email: z.string().trim().min(1, 'Email is required').email('Enter a valid email address'),
  password: z.string().min(8, 'Password must be at least 8 characters'),
  display_name: z.string().trim().min(1, 'Display name is required'),
})
export type RegisterFormValues = z.infer<typeof registerSchema>

// --- Password reset (post-Phase-10 addition) ---

export const forgotPasswordSchema = z.object({
  email: z.string().trim().min(1, 'Email is required').email('Enter a valid email address'),
})
export type ForgotPasswordFormValues = z.infer<typeof forgotPasswordSchema>

export const resetPasswordSchema = z.object({
  new_password: z.string().min(8, 'Password must be at least 8 characters'),
})
export type ResetPasswordFormValues = z.infer<typeof resetPasswordSchema>

export const settingsSchema = z.object({
  display_name: z.string().trim().min(1, 'Player name is required'),
})
export type SettingsFormValues = z.infer<typeof settingsSchema>

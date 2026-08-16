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

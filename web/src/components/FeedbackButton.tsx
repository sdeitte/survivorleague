import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import * as Dialog from '@radix-ui/react-dialog'
import { sendFeedback, ApiError } from '../api'

// A small, app-wide "Provide feedback" entry point — one modal, one
// textarea, POSTs to /feedback which forwards it straight to the admin
// inbox (see api/internal/notify.Service.SendFeedbackEmail). No feedback
// history/list anywhere in the app; this is fire-and-forget by design.
export function FeedbackButton() {
  const [open, setOpen] = useState(false)
  const [message, setMessage] = useState('')

  const sendMutation = useMutation({
    mutationFn: (msg: string) => sendFeedback(msg),
    onSuccess: () => {
      setMessage('')
      setOpen(false)
    },
  })

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) sendMutation.reset()
      }}
    >
      <Dialog.Trigger asChild>
        <button type="button" className="text-xs text-slate-500 underline">
          Feedback
        </button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/60" />
        <Dialog.Content className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-sm rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-4">
          <Dialog.Title className="text-lg font-semibold text-slate-100">Provide feedback</Dialog.Title>
          <Dialog.Description className="text-sm text-slate-500">
            Bug reports, feature requests, anything — this goes straight to the admin.
          </Dialog.Description>
          <textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            rows={5}
            maxLength={5000}
            placeholder="What's on your mind?"
            className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-slate-100 placeholder:text-slate-600"
          />
          {sendMutation.error && (
            <p className="text-sm text-red-400">
              {sendMutation.error instanceof ApiError ? sendMutation.error.message : 'Failed to send feedback.'}
            </p>
          )}
          <div className="flex gap-2">
            <Dialog.Close asChild>
              <button type="button" className="flex-1 rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100">
                Cancel
              </button>
            </Dialog.Close>
            <button
              type="button"
              disabled={!message.trim() || sendMutation.isPending}
              onClick={() => sendMutation.mutate(message.trim())}
              className="flex-1 rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
            >
              {sendMutation.isPending ? 'Sending…' : 'Send'}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

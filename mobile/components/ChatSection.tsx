import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { ApiError } from '../api';

// The league smack-talk feed — mirrors web/src/components/LeagueChat.tsx.
// A height-bounded, independently-scrolling box (a plain ScrollView, not
// another FlatList, since this renders inside LeagueDetailScreen's own
// FlatList header — nesting two same-orientation VirtualizedLists is what
// RN's "VirtualizedLists should never be nested" warning is about, and a
// bounded ScrollView here isn't one). Messages older than 7 days are
// already excluded server-side (see internal/chat.Service) — nothing
// client-side to filter. Polls every 10s. No automated moderation —
// isCommissioner gets a delete affordance per message instead.
export function ChatSection({ leagueId, isCommissioner }: { leagueId: string; isCommissioner: boolean }) {
  const { authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState('');

  const messagesQuery = useQuery({
    queryKey: ['league', leagueId, 'messages'],
    queryFn: () => authFetch((token) => api.listMessages(token, leagueId)),
    refetchInterval: 10_000,
  });

  const postMutation = useMutation({
    mutationFn: (body: string) => authFetch((token) => api.postMessage(token, leagueId, body)),
    onSuccess: (msg) => {
      setDraft('');
      queryClient.setQueryData(['league', leagueId, 'messages'], (old: typeof messagesQuery.data) =>
        old ? [...old, msg] : [msg],
      );
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (messageId: string) => authFetch((token) => api.deleteMessage(token, leagueId, messageId)),
    onSuccess: (_data, messageId) => {
      queryClient.setQueryData(['league', leagueId, 'messages'], (old: typeof messagesQuery.data) =>
        old?.filter((m) => m.id !== messageId),
      );
    },
  });

  const submit = () => {
    const body = draft.trim();
    if (!body || postMutation.isPending) return;
    postMutation.mutate(body);
  };

  return (
    <View style={styles.card}>
      <Text style={styles.title}>League chat</Text>

      <ScrollView style={styles.messages} nestedScrollEnabled>
        {messagesQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
        {messagesQuery.error && (
          <Text style={styles.error}>
            {messagesQuery.error instanceof ApiError ? messagesQuery.error.message : 'Could not load chat.'}
          </Text>
        )}
        {messagesQuery.data?.length === 0 && <Text style={styles.empty}>No messages yet — say something.</Text>}
        {messagesQuery.data?.map((m) => (
          <View key={m.id} style={styles.messageRow}>
            <Text style={styles.messageText}>
              <Text style={styles.messageAuthor}>{m.team_name || m.display_name}</Text> {m.body}
            </Text>
            {isCommissioner && (
              <Pressable onPress={() => deleteMutation.mutate(m.id)} disabled={deleteMutation.isPending}>
                <Text style={styles.deleteLink}>delete</Text>
              </Pressable>
            )}
          </View>
        ))}
      </ScrollView>

      <View style={styles.composerRow}>
        <TextInput
          value={draft}
          onChangeText={setDraft}
          placeholder="Say something…"
          placeholderTextColor="#475569"
          maxLength={1000}
          style={styles.input}
          onSubmitEditing={submit}
        />
        <Pressable
          style={[styles.sendButton, (!draft.trim() || postMutation.isPending) && styles.buttonDisabled]}
          disabled={!draft.trim() || postMutation.isPending}
          onPress={submit}
        >
          <Text style={styles.sendButtonText}>Send</Text>
        </Pressable>
      </View>
      {postMutation.error && (
        <Text style={styles.error}>
          {postMutation.error instanceof ApiError ? postMutation.error.message : 'Failed to send message.'}
        </Text>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 16,
    gap: 10,
    marginBottom: 12,
  },
  title: {
    color: '#f1f5f9',
    fontSize: 14,
    fontWeight: '600',
  },
  messages: {
    maxHeight: 220,
  },
  messageRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    gap: 8,
    paddingVertical: 4,
  },
  messageText: {
    color: '#f1f5f9',
    fontSize: 13,
    flexShrink: 1,
  },
  messageAuthor: {
    color: '#94a3b8',
    fontWeight: '600',
  },
  deleteLink: {
    color: '#64748b',
    fontSize: 11,
  },
  empty: {
    color: '#64748b',
    fontSize: 12,
  },
  composerRow: {
    flexDirection: 'row',
    gap: 8,
    alignItems: 'center',
  },
  input: {
    flex: 1,
    backgroundColor: '#0f172a',
    borderRadius: 8,
    paddingVertical: 8,
    paddingHorizontal: 10,
    color: '#f1f5f9',
    fontSize: 13,
  },
  sendButton: {
    backgroundColor: '#f1f5f9',
    borderRadius: 8,
    paddingVertical: 8,
    paddingHorizontal: 14,
  },
  sendButtonText: {
    color: '#0f172a',
    fontWeight: '600',
    fontSize: 13,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  error: {
    color: '#f87171',
    fontSize: 12,
  },
});

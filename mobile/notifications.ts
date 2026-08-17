// Phase 7: Expo push token registration. This is real device-token
// registration code (matches the backend's POST /me/device-tokens
// contract in api.ts's registerDeviceToken), but there is no EAS project
// configured in this environment (app.json has no extra.eas.projectId)
// and no physical device/APNs/FCM to test against — see the repo's
// README/report for that open item. registerForPushNotificationsAsync
// below is written to fail soft (return null, log a warning) rather than
// throw, specifically so that gap doesn't break login on web/simulator/
// any environment without real push plumbing.
import Constants from 'expo-constants';
import * as Device from 'expo-device';
import * as Notifications from 'expo-notifications';
import { Platform } from 'react-native';

// Show notifications while the app is foregrounded — otherwise
// expo-notifications suppresses them by default on some platforms.
Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowBanner: true,
    shouldShowList: true,
    shouldPlaySound: false,
    shouldSetBadge: false,
  }),
});

/**
 * Requests notification permission and returns a fresh Expo push token, or
 * null if permission was denied, this isn't a real device (Device.isDevice
 * is false on simulators/emulators — Expo push tokens aren't obtainable
 * there), the platform is web (push is mobile-only per the plan's stack),
 * or no EAS projectId is configured yet. Never throws — every failure
 * mode here is expected/recoverable, not a bug to crash the app over.
 */
export async function registerForPushNotificationsAsync(): Promise<{
  token: string;
  platform: 'ios' | 'android';
} | null> {
  if (Platform.OS !== 'ios' && Platform.OS !== 'android') {
    return null; // web has no push capability per the plan's stack.
  }

  if (!Device.isDevice) {
    console.warn('registerForPushNotificationsAsync: not a physical device (simulator/emulator) — skipping.');
    return null;
  }

  if (Platform.OS === 'android') {
    await Notifications.setNotificationChannelAsync('default', {
      name: 'default',
      importance: Notifications.AndroidImportance.MAX,
      vibrationPattern: [0, 250, 250, 250],
      lightColor: '#FF231F7C',
    });
  }

  const { status: existingStatus } = await Notifications.getPermissionsAsync();
  let finalStatus = existingStatus;
  if (existingStatus !== 'granted') {
    const { status } = await Notifications.requestPermissionsAsync();
    finalStatus = status;
  }
  if (finalStatus !== 'granted') {
    console.warn('registerForPushNotificationsAsync: notification permission not granted.');
    return null;
  }

  const projectId: string | undefined =
    Constants?.expoConfig?.extra?.eas?.projectId ?? Constants?.easConfig?.projectId;
  if (!projectId) {
    // Expected in this environment — no EAS project has been created yet
    // (see api.ts / mobile/README.md's open-items note). Real push
    // registration needs `eas init` / app.json's extra.eas.projectId set
    // before this can succeed against a real device.
    console.warn('registerForPushNotificationsAsync: no EAS projectId configured — cannot obtain an Expo push token yet.');
    return null;
  }

  try {
    const { data } = await Notifications.getExpoPushTokenAsync({ projectId });
    return { token: data, platform: Platform.OS };
  } catch (err) {
    console.warn('registerForPushNotificationsAsync: getExpoPushTokenAsync failed:', err);
    return null;
  }
}

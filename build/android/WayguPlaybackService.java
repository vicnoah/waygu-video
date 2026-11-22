package dev.oneo.nsgo;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.media.MediaMetadata;
import android.media.session.MediaSession;
import android.media.session.PlaybackState;
import android.os.Build;
import android.os.IBinder;
import android.os.PowerManager;
import java.util.Map;

/** Keeps the activity's existing decoder alive while its video surface is away. */
public final class WayguPlaybackService extends Service {
    private static final String CHANNEL = "waygu-playback";
    private static final String PAUSE = "waygu.playback.background.pause";
    private static final int NOTIFICATION_ID = 7241;
    // Host calls and service callbacks all run on the main thread.
    private static Lease wanted;
    private static WayguPlaybackService active;
    private Lease serving;
    private MediaSession session;
    private PowerManager.WakeLock wake;

    private record Lease(String token, String title, String channel, String pause,
                         NsgoView view, NsgoView.HostReply reply) {}

    static void bind(NsgoActivity activity, NsgoView view) {
        view.handle("waygu.playback.background.supported", (data, reply) ->
                reply.resolve(activity.getPackageManager().resolveService(
                        new Intent(activity, WayguPlaybackService.class), 0) != null));
        view.handle("waygu.playback.background.start", (data, reply) -> {
            if (!(data instanceof Map<?, ?> fields) || !(fields.get("token") instanceof String token)
                    || token.isEmpty() || !(fields.get("title") instanceof String title)
                    || !(fields.get("channel") instanceof String channel)
                    || !(fields.get("pause") instanceof String pause)) {
                reply.resolve(false);
                return;
            }
            Lease lease = new Lease(token, title, channel, pause, view, reply);
            wanted = lease;
            Intent start = new Intent(activity, WayguPlaybackService.class)
                    .putExtra("token", token).putExtra("title", title).putExtra("channel", channel);
            try {
                activity.startForegroundService(start);
            } catch (RuntimeException e) {
                wanted = null;
                if (active != null) active.stopSelf();
                reply.resolve(false);
            }
        });
        view.handle("waygu.playback.background.stop", (data, reply) -> {
            if (wanted != null && wanted.view() == view && wanted.token().equals(data)) {
                cancel(wanted);
            }
            reply.resolve(true);
        });
    }

    static void detach(NsgoView view) {
        if (wanted != null && wanted.view() == view) cancel(wanted);
    }

    private static void cancel(Lease lease) {
        if (wanted == lease) wanted = null;
        lease.reply().resolve(false);
        if (active != null && active.serving == lease) active.stopSelf();
    }

    @Override
    public IBinder onBind(Intent intent) { return null; }

    @Override
    public int onStartCommand(Intent intent, int flags, int id) {
        if (intent == null) {
            stopSelf(id);
            return START_NOT_STICKY;
        }
        String token = intent.getStringExtra("token");
        if (PAUSE.equals(intent.getAction())) {
            if (serving != null && serving == wanted && serving.token().equals(token)) pause();
            return START_NOT_STICKY;
        }
        active = this;
        Lease lease = wanted;
        if (lease == null || !lease.token().equals(token)) {
            // Complete the foreground-start contract even if the player already closed.
            publish(intent.getStringExtra("title"), intent.getStringExtra("channel"), null);
            stopSelf(id);
            return START_NOT_STICKY;
        }
        serving = lease;
        if (session == null) {
            session = new MediaSession(this, "WayguPlayback");
            session.setCallback(new MediaSession.Callback() {
                @Override public void onPause() { pause(); }
                @Override public void onStop() { pause(); }
            });
        }
        session.setMetadata(new MediaMetadata.Builder().putString(MediaMetadata.METADATA_KEY_TITLE, lease.title()).build());
        session.setPlaybackState(new PlaybackState.Builder()
                .setActions(PlaybackState.ACTION_PAUSE | PlaybackState.ACTION_STOP)
                .setState(PlaybackState.STATE_PLAYING, PlaybackState.PLAYBACK_POSITION_UNKNOWN, 1).build());
        session.setActive(true);
        publish(lease.title(), lease.channel(), lease);
        if (wake == null) {
            wake = getSystemService(PowerManager.class)
                    .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, getPackageName() + ":playback");
            wake.acquire();
        }
        lease.reply().resolve(true);
        return START_NOT_STICKY;
    }

    private void publish(String title, String channel, Lease lease) {
        getSystemService(NotificationManager.class).createNotificationChannel(
                new NotificationChannel(CHANNEL, channel, NotificationManager.IMPORTANCE_LOW));
        PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, NsgoActivity.class)
                .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP),
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Notification.Builder notice = new Notification.Builder(this, CHANNEL)
                .setSmallIcon(android.R.drawable.ic_media_play).setContentTitle(title).setContentText(channel)
                .setOnlyAlertOnce(true).setOngoing(true).setContentIntent(open)
                .setVisibility(Notification.VISIBILITY_PRIVATE);
        if (lease != null) {
            Intent pause = new Intent(this, WayguPlaybackService.class).setAction(PAUSE)
                    .putExtra("token", lease.token());
            PendingIntent action = PendingIntent.getService(this, 0, pause,
                    PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
            notice.addAction(new Notification.Action.Builder(android.R.drawable.ic_media_pause, lease.pause(), action).build())
                    .setStyle(new Notification.MediaStyle().setMediaSession(session.getSessionToken()).setShowActionsInCompactView(0));
        }
        if (Build.VERSION.SDK_INT >= 29) {
            startForeground(NOTIFICATION_ID, notice.build(), ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK);
        } else {
            startForeground(NOTIFICATION_ID, notice.build());
        }
    }

    private void pause() {
        if (serving == null || serving != wanted) return;
        serving.view().send(PAUSE, serving.token());
        cancel(serving);
    }

    @Override
    public void onTaskRemoved(Intent root) {
        pause();
        stopSelf();
    }

    @Override
    public void onDestroy() {
        if (active == this) active = null;
        if (serving != null && serving == wanted) {
            serving.view().send(PAUSE, serving.token());
            wanted = null;
        }
        if (session != null) session.release();
        if (wake != null && wake.isHeld()) wake.release();
        stopForeground(STOP_FOREGROUND_REMOVE);
        super.onDestroy();
    }
}

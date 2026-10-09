#define _GNU_SOURCE
#define _XOPEN_SOURCE 700
#define _POSIX_C_SOURCE 200809L
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <locale.h>
#include <pty.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/ioctl.h>
#include <sys/select.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <termios.h>
#include <time.h>
#include <unistd.h>
#include <wchar.h>

#define IFACE "wlan0"
#define MAX_NETWORKS 128
#define ROW_SIZE 512
#define SCAN_OUTPUT_SIZE 65536

typedef struct {
    char ssid[256];
    char bssid[32];
    char security[64];
    int signal;
} Network;

typedef struct {
    Network networks[MAX_NETWORKS];
    int count;
    int selected;
    int offset;
    int connected;
    int connecting;
    int password_prompt;
    int password_bytes;
    int help;
    int pty_fd;
    pid_t child;
    int scan_fd;
    pid_t scan_child;
    int scan_active;
    char scan_output[SCAN_OUTPUT_SIZE];
    size_t scan_output_size;
    char scan_selection[256];
    char active_ssid[256];
    char target_ssid[256];
    char ipv4[64];
    char state[128];
    char message[256];
    char child_error[256];
    char child_line[512];
    size_t child_line_size;
    int escape_sequence;
} App;

static struct termios original_termios;
static int terminal_active;

static int run_command(const char *command) {
    return system(command);
}

static void restore_terminal(void) {
    if (!terminal_active) return;
    tcsetattr(STDIN_FILENO, TCSAFLUSH, &original_termios);
    terminal_active = 0;
    fputs("\033[0m\033[?25h\033[?1049l", stdout);
    fflush(stdout);
}

static void on_signal(int signal_number) {
    restore_terminal();
    _exit(128 + signal_number);
}

static void copy_text(char *destination, size_t size, const char *source) {
    if (!size) return;
    size_t length = strlen(source);
    if (length >= size) length = size - 1;
    memcpy(destination, source, length);
    destination[length] = '\0';
}

static void split_row(const char *row, char fields[4][256]) {
    size_t field = 0;
    size_t offset = 0;
    for (size_t i = 0; row[i] && field < 4; ++i) {
        char ch = row[i];
        if (ch == '\\' && row[i + 1]) ch = row[++i];
        else if (ch == ':') {
            fields[field][offset] = '\0';
            ++field;
            offset = 0;
            continue;
        } else if (ch == '\n' || ch == '\r') break;
        if (offset < 254) fields[field][offset++] = ch;
    }
    if (field < 4) fields[field][offset] = '\0';
}

static int parse_networks(char *output, Network networks[MAX_NETWORKS]) {
    int count = 0;
    char *save = NULL;
    for (char *row = strtok_r(output, "\n", &save); row; row = strtok_r(NULL, "\n", &save)) {
        if (strlen(row) >= ROW_SIZE) continue;
        char fields[4][256] = {{0}};
        split_row(row, fields);
        if (!fields[0][0] || !fields[1][0]) continue;
        int signal = atoi(fields[2]);
        int found = -1;
        for (int i = 0; i < count; ++i) {
            if (!strcmp(networks[i].ssid, fields[0])) {
                found = i;
                break;
            }
        }
        if (found >= 0) {
            if (signal > networks[found].signal) {
                networks[found].signal = signal;
                copy_text(networks[found].bssid, sizeof(networks[found].bssid), fields[1]);
                copy_text(networks[found].security, sizeof(networks[found].security), fields[3]);
            }
            continue;
        }
        if (count == MAX_NETWORKS) continue;
        copy_text(networks[count].ssid, sizeof(networks[count].ssid), fields[0]);
        copy_text(networks[count].bssid, sizeof(networks[count].bssid), fields[1]);
        copy_text(networks[count].security, sizeof(networks[count].security), fields[3]);
        networks[count].signal = signal;
        ++count;
    }
    for (int i = 1; i < count; ++i) {
        Network current = networks[i];
        int j = i - 1;
        while (j >= 0 && networks[j].signal < current.signal) {
            networks[j + 1] = networks[j];
            --j;
        }
        networks[j + 1] = current;
    }
    return count;
}

static int refresh_connection(App *app) {
    FILE *pipe = popen("nmcli -g GENERAL.STATE,GENERAL.CONNECTION,IP4.ADDRESS device show wlan0 2>/dev/null", "r");
    if (!pipe) return 0;
    char state[128] = {0};
    char ssid[256] = {0};
    char address[64] = {0};
    if (fgets(state, sizeof(state), pipe)) state[strcspn(state, "\r\n")] = 0;
    if (fgets(ssid, sizeof(ssid), pipe)) ssid[strcspn(ssid, "\r\n")] = 0;
    if (fgets(address, sizeof(address), pipe)) address[strcspn(address, "\r\n")] = 0;
    pclose(pipe);
    int connected = !strncmp(state, "100", 3);
    if (!connected || !strcmp(ssid, "--")) ssid[0] = 0;
    if (!strcmp(address, "--")) address[0] = 0;
    int changed = app->connected != connected ||
                  strcmp(app->active_ssid, ssid) != 0 || strcmp(app->ipv4, address) != 0;
    app->connected = connected;
    copy_text(app->active_ssid, sizeof(app->active_ssid), ssid);
    copy_text(app->ipv4, sizeof(app->ipv4), address);
    if (app->connected) snprintf(app->state, sizeof(app->state), "CONNECTED  %s", ssid[0] ? ssid : IFACE);
    else if (app->connecting) copy_text(app->state, sizeof(app->state), "CONNECTING");
    else if (strstr(state, "unavailable")) copy_text(app->state, sizeof(app->state), "RADIO UNAVAILABLE");
    else copy_text(app->state, sizeof(app->state), "DISCONNECTED");
    return changed;
}

static int has_password_prompt(const char *text) {
    return strcasestr(text, "password") || strcasestr(text, "passphrase") || strstr(text, "密码") || strstr(text, "口令");
}

static void copy_status_line(App *app) {
    while (app->child_line_size && isspace((unsigned char)app->child_line[app->child_line_size - 1]))
        app->child_line[--app->child_line_size] = 0;
    if (!app->child_line_size) return;
    app->child_line[app->child_line_size] = 0;
    if (has_password_prompt(app->child_line)) {
        app->password_prompt = 1;
        snprintf(app->message, sizeof(app->message), "PASSWORD REQUIRED  %s", app->child_line);
        app->password_bytes = 0;
    } else if (!app->password_prompt) {
        copy_text(app->message, sizeof(app->message), app->child_line);
        copy_text(app->child_error, sizeof(app->child_error), app->child_line);
    }
    app->child_line_size = 0;
    app->child_line[0] = 0;
}

static void read_child_output(App *app) {
    char buffer[512];
    ssize_t count = read(app->pty_fd, buffer, sizeof(buffer));
    if (count <= 0) return;
    for (ssize_t i = 0; i < count; ++i) {
        unsigned char ch = (unsigned char)buffer[i];
        if (app->escape_sequence) {
            if (ch >= 0x40 && ch <= 0x7e) app->escape_sequence = 0;
            continue;
        }
        if (ch == 27) {
            app->escape_sequence = 1;
            continue;
        }
        if (ch == '\r' || ch == '\n') {
            copy_status_line(app);
            continue;
        }
        if ((ch >= 32 || ch >= 128) && app->child_line_size < sizeof(app->child_line) - 1) {
            app->child_line[app->child_line_size++] = (char)ch;
            app->child_line[app->child_line_size] = 0;
            if (has_password_prompt(app->child_line)) {
                app->password_prompt = 1;
                snprintf(app->message, sizeof(app->message), "PASSWORD REQUIRED");
                app->child_line_size = 0;
                app->child_line[0] = 0;
            }
        }
    }
}

static void print_spaces(int count) {
    while (count-- > 0) putchar(' ');
}

static void print_cell(const char *text, int columns) {
    mbstate_t state = {0};
    const char *cursor = text;
    int used = 0;
    while (*cursor && used < columns) {
        wchar_t character;
        size_t bytes = mbrtowc(&character, cursor, MB_CUR_MAX, &state);
        if (bytes == (size_t)-1 || bytes == (size_t)-2) {
            memset(&state, 0, sizeof(state));
            putchar('?');
            ++cursor;
            ++used;
            continue;
        }
        if (!bytes) break;
        int cells = wcwidth(character);
        if (cells < 0) cells = character >= 0x1100 ? 2 : 1;
        if (used + cells > columns) break;
        fwrite(cursor, 1, bytes, stdout);
        cursor += bytes;
        used += cells;
    }
    print_spaces(columns - used);
}

static int terminal_columns(void) {
    struct winsize size = {0};
    ioctl(STDOUT_FILENO, TIOCGWINSZ, &size);
    return size.ws_col > 20 ? size.ws_col : 80;
}

static int terminal_rows(void) {
    struct winsize size = {0};
    ioctl(STDOUT_FILENO, TIOCGWINSZ, &size);
    return size.ws_row > 10 ? size.ws_row : 24;
}

static void border(int left, int width) {
    print_spaces(left);
    fputs("\033[96m+", stdout);
    for (int i = 0; i < width - 2; ++i) putchar('-');
    fputs("+\033[0m\033[K\r\n", stdout);
}

static void panel_line(int left, int width, const char *text, const char *color) {
    print_spaces(left);
    fputs("\033[96m|\033[0m ", stdout);
    if (color) fputs(color, stdout);
    print_cell(text, width - 4);
    fputs("\033[0m \033[96m|\033[0m\033[K\r\n", stdout);
}

static int row_is_connected(const App *app, const Network *network) {
    return app->connected && app->active_ssid[0] && !strcmp(app->active_ssid, network->ssid);
}

static void draw(App *app) {
    int columns = terminal_columns();
    int width = columns > 102 ? 100 : columns - 2;
    if (width < 48) width = 48;
    int left = columns > width ? (columns - width) / 2 : 0;
    int page = terminal_rows() - 13;
    if (page < 2) page = 2;
    int ssid_width = width - 45;
    fputs("\033[H\033[?25l\033[0m", stdout);
    border(left, width);
    panel_line(left, width, "WIFI-MENU  /  AX210", "\033[1;96m");
    char header[256];
    snprintf(header, sizeof(header), "IFACE %s   NETWORKMANAGER   2.4 / 5 / 6 GHz", IFACE);
    panel_line(left, width, header, "\033[90m");
    if (app->connected) {
        snprintf(header, sizeof(header), "● CONNECTED   %s   IPv4 %s", app->active_ssid[0] ? app->active_ssid : IFACE, app->ipv4[0] ? app->ipv4 : "pending");
        panel_line(left, width, header, "\033[1;92m");
        if (app->connecting) {
            snprintf(header, sizeof(header), "◌ CONNECTING TO   %s", app->target_ssid);
            panel_line(left, width, header, "\033[1;93m");
        }
    } else if (app->connecting) {
        snprintf(header, sizeof(header), "◌ CONNECTING TO   %s", app->target_ssid[0] ? app->target_ssid : "selected access point");
        panel_line(left, width, header, "\033[1;93m");
    } else {
        panel_line(left, width, "○ DISCONNECTED", "\033[90m");
    }
    border(left, width);
    print_spaces(left);
    fputs("\033[96m|\033[0m  ", stdout);
    print_cell("SIG", 5);
    print_cell("SSID", ssid_width);
    fputs(" \033[96m|\033[0m ", stdout);
    print_cell("SECURITY", 10);
    fputs(" \033[96m|\033[0m ", stdout);
    print_cell("BSSID", 17);
    fputs(" \033[96m|\033[0m\r\n", stdout);
    for (int row = 0; row < page && app->offset + row < app->count; ++row) {
        int index = app->offset + row;
        Network *network = &app->networks[index];
        print_spaces(left);
        if (index == app->selected) fputs("\033[1;30;102m", stdout);
        else if (row_is_connected(app, network)) fputs("\033[1;92m", stdout);
        fputs("| ", stdout);
        fputs(row_is_connected(app, network) ? "● " : (index == app->selected ? "> " : "  "), stdout);
        char strength[16];
        snprintf(strength, sizeof(strength), "%3d%% ", network->signal);
        print_cell(strength, 5);
        print_cell(network->ssid, ssid_width);
        fputs(" | ", stdout);
        print_cell(network->security[0] ? network->security : "OPEN", 10);
        fputs(" | ", stdout);
        print_cell(network->bssid, 17);
        fputs(" |\033[0m\033[K\r\n", stdout);
    }
    if (!app->count) panel_line(left, width, "NO VISIBLE NETWORKS  |  r rescan  |  q quit", "\033[1;93m");
    border(left, width);
    if (app->help) {
        panel_line(left, width, "↑/↓ or j/k  Select network", "\033[1;96m");
        panel_line(left, width, "Enter        Connect or activate selected network", "\033[1;96m");
        panel_line(left, width, "d            Disconnect wlan0", "\033[1;96m");
        panel_line(left, width, "r            Rescan   |   Esc cancel   |   q quit", "\033[1;96m");
        panel_line(left, width, "Press any key to return", "\033[90m");
    } else {
        char footer[256];
        if (app->connecting && app->password_prompt) {
            int masked_width = width - 50;
            if (masked_width < 0) masked_width = 0;
            int masked = app->password_bytes > masked_width ? masked_width : app->password_bytes;
            snprintf(footer, sizeof(footer), "PASSWORD  %.*s  Enter submit  Esc cancel", masked, "********************************************************************************************************************************");
            panel_line(left, width, footer, "\033[1;93m");
        } else if (app->connecting) {
            panel_line(left, width, "WAITING FOR NETWORKMANAGER  |  d / Esc / Ctrl-C cancel", "\033[1;93m");
        } else if (app->scan_active) {
            panel_line(left, width, "SCANNING ACCESS POINTS  |  Esc cancel", "\033[1;93m");
        } else {
            panel_line(left, width, "↑/↓ select   Enter connect   d disconnect   r rescan   ? help   q quit", "\033[90m");
        }
        if (app->message[0]) panel_line(left, width, app->message, "\033[1;96m");
    }
    border(left, width);
    fputs("\033[J", stdout);
    fflush(stdout);
}

static int run_nmcli(char *const args[]) {
    pid_t child = fork();
    if (child == -1) return 0;
    if (child == 0) {
        execvp(args[0], args);
        _exit(127);
    }
    int status = 0;
    while (waitpid(child, &status, 0) == -1) {
        if (errno != EINTR) return 0;
    }
    return WIFEXITED(status) && WEXITSTATUS(status) == 0;
}

static int network_is_protected(const char *security) {
    return strstr(security, "WPA") || strstr(security, "WEP");
}

static int profile_matches_ssid(const char *uuid, const char *name, const char *ssid) {
    if (!strcmp(name, ssid)) return 1;
    char query[192];
    snprintf(query, sizeof(query), "nmcli --get-values 802-11-wireless.ssid connection show uuid %s 2>/dev/null", uuid);
    FILE *pipe = popen(query, "r");
    if (!pipe) return 0;
    char value[256] = {0};
    if (fgets(value, sizeof(value), pipe)) value[strcspn(value, "\r\n")] = 0;
    pclose(pipe);
    return !strcmp(value, ssid);
}

static int prepare_profile(const char *ssid, const char *security, char *uuid, size_t uuid_size) {
    FILE *pipe = popen("nmcli --terse --escape yes --fields UUID,TYPE,NAME connection show 2>/dev/null", "r");
    if (!pipe) return 0;
    char row[ROW_SIZE];
    char found_uuid[64] = {0};
    while (fgets(row, sizeof(row), pipe)) {
        char fields[4][256] = {{0}};
        split_row(row, fields);
        if (!strcmp(fields[1], "802-11-wireless") && profile_matches_ssid(fields[0], fields[2], ssid)) {
            char *args[] = {"nmcli", "connection", "modify", "uuid", fields[0], "connection.autoconnect", "no", NULL};
            char *secure_args[] = {"nmcli", "connection", "modify", "uuid", fields[0], "connection.autoconnect", "no", "802-11-wireless-security.psk-flags", "2", "802-11-wireless-security.psk", "", NULL};
            if (!run_nmcli(network_is_protected(security) ? secure_args : args)) {
                pclose(pipe);
                return 0;
            }
            if (!found_uuid[0] || !strcmp(fields[2], ssid)) copy_text(found_uuid, sizeof(found_uuid), fields[0]);
        }
    }
    pclose(pipe);
    int protected_network = network_is_protected(security);
    if (found_uuid[0]) {
        copy_text(uuid, uuid_size, found_uuid);
        return 1;
    }
    char profile[64];
    snprintf(profile, sizeof(profile), "wifi-menu-%ld", (long)getpid());
    char *base_args[] = {"nmcli", "connection", "add", "type", "wifi", "con-name", profile, "ifname", (char *)IFACE, "ssid", (char *)ssid, "connection.autoconnect", "no", "ipv4.method", "auto", "ipv6.method", "auto", NULL};
    char *wpa_args[] = {"nmcli", "connection", "add", "type", "wifi", "con-name", profile, "ifname", (char *)IFACE, "ssid", (char *)ssid, "connection.autoconnect", "no", "ipv4.method", "auto", "ipv6.method", "auto", "802-11-wireless-security.key-mgmt", "wpa-psk", "802-11-wireless-security.psk-flags", "2", NULL};
    char *sae_args[] = {"nmcli", "connection", "add", "type", "wifi", "con-name", profile, "ifname", (char *)IFACE, "ssid", (char *)ssid, "connection.autoconnect", "no", "ipv4.method", "auto", "ipv6.method", "auto", "802-11-wireless-security.key-mgmt", "sae", "802-11-wireless-security.psk-flags", "2", NULL};
    char **create_args;
    if (protected_network) create_args = strstr(security, "WPA3") && !strstr(security, "WPA2") ? sae_args : wpa_args;
    else create_args = base_args;
    if (!run_nmcli(create_args)) return 0;
    char query[128];
    snprintf(query, sizeof(query), "nmcli --get-values connection.uuid connection show id %s 2>/dev/null", profile);
    pipe = popen(query, "r");
    if (!pipe) return 0;
    char value[128] = {0};
    if (fgets(value, sizeof(value), pipe)) value[strcspn(value, "\r\n")] = 0;
    pclose(pipe);
    if (!value[0]) return 0;
    copy_text(uuid, uuid_size, value);
    return 1;
}

static int begin_connection(App *app, const Network *network) {
    int master = -1;
    int slave = -1;
    if (openpty(&master, &slave, NULL, NULL, NULL) == -1) return 0;
    pid_t child = fork();
    if (child == -1) {
        close(master);
        close(slave);
        return 0;
    }
    if (child == 0) {
        close(master);
        setsid();
        ioctl(slave, TIOCSCTTY, 0);
        setenv("LC_ALL", "C", 1);
        struct termios settings;
        if (tcgetattr(slave, &settings) == 0) {
            settings.c_lflag &= (tcflag_t)~ECHO;
            tcsetattr(slave, TCSANOW, &settings);
        }
        dup2(slave, STDIN_FILENO);
        dup2(slave, STDOUT_FILENO);
        dup2(slave, STDERR_FILENO);
        if (slave > STDERR_FILENO) close(slave);
        int tty_in = dup(STDIN_FILENO);
        int tty_out = dup(STDOUT_FILENO);
        int tty_err = dup(STDERR_FILENO);
        int null_fd = open("/dev/null", O_RDWR);
        if (null_fd >= 0) {
            dup2(null_fd, STDIN_FILENO);
            dup2(null_fd, STDOUT_FILENO);
            dup2(null_fd, STDERR_FILENO);
            if (null_fd > STDERR_FILENO) close(null_fd);
        }
        char uuid[128] = {0};
        int prepared = prepare_profile(network->ssid, network->security, uuid, sizeof(uuid));
        if (tty_in >= 0) dup2(tty_in, STDIN_FILENO);
        if (tty_out >= 0) dup2(tty_out, STDOUT_FILENO);
        if (tty_err >= 0) dup2(tty_err, STDERR_FILENO);
        if (tty_in > STDERR_FILENO) close(tty_in);
        if (tty_out > STDERR_FILENO) close(tty_out);
        if (tty_err > STDERR_FILENO) close(tty_err);
        if (!prepared) {
            dprintf(STDOUT_FILENO, "Could not prepare a no-save NetworkManager profile\r\n");
            _exit(1);
        }
        char *args[] = {"nmcli", "--ask", "connection", "up", "uuid", uuid, "ifname", (char *)IFACE, NULL};
        execvp(args[0], args);
        _exit(127);
    }
    close(slave);
    int flags = fcntl(master, F_GETFL, 0);
    if (flags >= 0) fcntl(master, F_SETFL, flags | O_NONBLOCK);
    app->pty_fd = master;
    app->child = child;
    app->connecting = 1;
    app->password_prompt = 0;
    app->password_bytes = 0;
    app->child_line_size = 0;
    app->child_error[0] = 0;
    copy_text(app->target_ssid, sizeof(app->target_ssid), network->ssid);
    snprintf(app->message, sizeof(app->message), "Preparing a profile that will not save the password");
    return 1;
}

static void finish_connection(App *app, int status) {
    copy_status_line(app);
    char target[256];
    copy_text(target, sizeof(target), app->target_ssid);
    if (app->pty_fd >= 0) close(app->pty_fd);
    app->pty_fd = -1;
    app->child = -1;
    app->connecting = 0;
    app->password_prompt = 0;
    app->password_bytes = 0;
    refresh_connection(app);
    if (status == 0 && app->connected) snprintf(app->message, sizeof(app->message), "Connected to %s", app->active_ssid[0] ? app->active_ssid : target);
    else if (status == 0) copy_text(app->message, sizeof(app->message), "Activation finished; waiting for network state");
    else if (app->child_error[0]) copy_text(app->message, sizeof(app->message), app->child_error);
    else snprintf(app->message, sizeof(app->message), "Connection failed (nmcli exit %d)", status);
    app->target_ssid[0] = 0;
}

static void cancel_connection(App *app) {
    if (app->child > 0) {
        kill(-app->child, SIGTERM);
        waitpid(app->child, NULL, 0);
    }
    if (app->pty_fd >= 0) close(app->pty_fd);
    app->pty_fd = -1;
    app->child = -1;
    app->connecting = 0;
    app->password_prompt = 0;
    app->password_bytes = 0;
    app->target_ssid[0] = 0;
    copy_text(app->message, sizeof(app->message), "Connection cancelled");
}

static int read_key(void) {
    unsigned char key;
    if (read(STDIN_FILENO, &key, 1) != 1) return -1;
    if (key != 27) return key;
    fd_set input;
    FD_ZERO(&input);
    FD_SET(STDIN_FILENO, &input);
    struct timeval timeout = {.tv_sec = 0, .tv_usec = 80000};
    if (select(STDIN_FILENO + 1, &input, NULL, NULL, &timeout) <= 0) return 27;
    unsigned char sequence[2];
    if (read(STDIN_FILENO, sequence, 2) != 2 || sequence[0] != '[') return 27;
    if (sequence[1] == 'A') return 1001;
    if (sequence[1] == 'B') return 1002;
    return 27;
}

static int begin_scan(App *app) {
    if (app->scan_active) return 0;
    int channel[2];
    if (pipe(channel) == -1) return 0;
    pid_t child = fork();
    if (child == -1) {
        close(channel[0]);
        close(channel[1]);
        return 0;
    }
    if (child == 0) {
        signal(SIGTERM, SIG_DFL);
        signal(SIGHUP, SIG_DFL);
        close(channel[0]);
        dup2(channel[1], STDOUT_FILENO);
        int null_fd = open("/dev/null", O_WRONLY);
        if (null_fd >= 0) dup2(null_fd, STDERR_FILENO);
        if (channel[1] > STDERR_FILENO) close(channel[1]);
        if (null_fd > STDERR_FILENO) close(null_fd);
        run_command("nmcli device wifi rescan ifname wlan0 >/dev/null 2>&1");
        usleep(3500000);
        char *args[] = {"nmcli", "--terse", "--escape", "yes", "--fields", "SSID,BSSID,SIGNAL,SECURITY", "device", "wifi", "list", "ifname", IFACE, NULL};
        execvp(args[0], args);
        _exit(127);
    }
    close(channel[1]);
    int flags = fcntl(channel[0], F_GETFL, 0);
    if (flags >= 0) fcntl(channel[0], F_SETFL, flags | O_NONBLOCK);
    app->scan_fd = channel[0];
    app->scan_child = child;
    app->scan_active = 1;
    app->scan_output_size = 0;
    app->scan_output[0] = 0;
    app->scan_selection[0] = 0;
    if (app->count && app->selected < app->count) copy_text(app->scan_selection, sizeof(app->scan_selection), app->networks[app->selected].ssid);
    copy_text(app->message, sizeof(app->message), "Requesting fresh Wi-Fi scan");
    return 1;
}

static void finish_scan(App *app, int status) {
    if (app->scan_fd >= 0) close(app->scan_fd);
    app->scan_fd = -1;
    app->scan_child = -1;
    app->scan_active = 0;
    if (status == 0) {
        app->scan_output[app->scan_output_size] = 0;
        int count = parse_networks(app->scan_output, app->networks);
        app->count = count < 0 ? 0 : count;
        app->selected = 0;
        for (int i = 0; i < app->count; ++i) {
            if (app->scan_selection[0] && !strcmp(app->scan_selection, app->networks[i].ssid)) app->selected = i;
        }
        app->offset = 0;
        snprintf(app->message, sizeof(app->message), "%d access point%s found", app->count, app->count == 1 ? "" : "s");
    } else {
        copy_text(app->message, sizeof(app->message), "Scan failed; check wlan0 and NetworkManager");
    }
}

static void cancel_scan(App *app) {
    if (app->scan_child > 0) {
        kill(app->scan_child, SIGTERM);
        waitpid(app->scan_child, NULL, 0);
    }
    if (app->scan_fd >= 0) close(app->scan_fd);
    app->scan_fd = -1;
    app->scan_child = -1;
    app->scan_active = 0;
    copy_text(app->message, sizeof(app->message), "Scan cancelled");
}

static int read_scan_output(App *app) {
    char buffer[4096];
    int changed = 0;
    for (;;) {
        ssize_t count = read(app->scan_fd, buffer, sizeof(buffer));
        if (count > 0) {
            size_t available = sizeof(app->scan_output) - app->scan_output_size - 1;
            size_t copy_size = (size_t)count < available ? (size_t)count : available;
            if (copy_size) {
                memcpy(app->scan_output + app->scan_output_size, buffer, copy_size);
                app->scan_output_size += copy_size;
                app->scan_output[app->scan_output_size] = 0;
            }
            changed = 1;
            continue;
        }
        if (count == 0) {
            close(app->scan_fd);
            app->scan_fd = -1;
            return 1;
        }
        if (errno == EINTR) continue;
        return changed;
    }
}

static void handle_key(App *app, int key) {
    if (app->help) {
        app->help = 0;
        return;
    }
    if (app->connecting) {
        if (key == 27 || key == 3 || (!app->password_prompt && (key == 'd' || key == 'D'))) {
            cancel_connection(app);
            return;
        }
        if (app->password_prompt) {
            if (key == 8 || key == 127) {
                if (app->password_bytes > 0) --app->password_bytes;
                char backspace = 127;
                ssize_t written = write(app->pty_fd, &backspace, 1);
                (void)written;
            } else if (key == 21) {
                app->password_bytes = 0;
                char kill_line = 21;
                ssize_t written = write(app->pty_fd, &kill_line, 1);
                (void)written;
            } else if (key == '\n' || key == '\r') {
                char enter = '\n';
                ssize_t written = write(app->pty_fd, &enter, 1);
                (void)written;
                app->password_prompt = 0;
                app->password_bytes = 0;
                copy_text(app->message, sizeof(app->message), "Authenticating and requesting an address");
            } else if (key >= 32 && key <= 255) {
                unsigned char ch = (unsigned char)key;
                if (write(app->pty_fd, &ch, 1) == 1) ++app->password_bytes;
            }
        }
        return;
    }
    if (app->scan_active && (key == 27 || key == 3)) {
        cancel_scan(app);
        return;
    }
    if (key == 'q' || key == 'Q') {
        if (app->connecting) cancel_connection(app);
        if (app->scan_active) cancel_scan(app);
        restore_terminal();
        exit(0);
    }
    if (key == '?' ) {
        app->help = 1;
        return;
    }
    if (key == 'r' || key == 'R') {
        if (!begin_scan(app)) copy_text(app->message, sizeof(app->message), "A scan is already running");
        return;
    }
    if ((key == 1001 || key == 'k') && app->count) app->selected = app->selected ? app->selected - 1 : app->count - 1;
    else if ((key == 1002 || key == 'j') && app->count) app->selected = app->selected + 1 < app->count ? app->selected + 1 : 0;
    else if ((key == 'd' || key == 'D') && app->connected) {
        copy_text(app->message, sizeof(app->message), "Disconnecting wlan0");
        draw(app);
        int command_status = run_command("nmcli device disconnect wlan0 >/dev/null 2>&1");
        (void)command_status;
        refresh_connection(app);
        copy_text(app->message, sizeof(app->message), "Disconnected");
    } else if ((key == '\n' || key == '\r') && app->count) {
        if (app->scan_active) {
            copy_text(app->message, sizeof(app->message), "Wait for the scan to finish");
            return;
        }
        if (!begin_connection(app, &app->networks[app->selected]))
            copy_text(app->message, sizeof(app->message), "Unable to start NetworkManager connection");
    } else if (key == 27) {
        app->message[0] = 0;
    }
    int page = terminal_rows() - 13;
    if (page < 2) page = 2;
    if (app->selected < app->offset) app->offset = app->selected;
    if (app->selected >= app->offset + page) app->offset = app->selected - page + 1;
}

int main(void) {
    setlocale(LC_ALL, "");
    if (geteuid() != 0) {
        fputs("Run as root: sudo wifi-menu\n", stderr);
        return 1;
    }
    if (!isatty(STDIN_FILENO) || !isatty(STDOUT_FILENO)) {
        fputs("wifi-menu requires an interactive terminal\n", stderr);
        return 1;
    }
    App app = {.pty_fd = -1, .child = -1, .scan_fd = -1, .scan_child = -1};
    if (tcgetattr(STDIN_FILENO, &original_termios) == -1) return 1;
    struct termios raw = original_termios;
    raw.c_lflag &= (tcflag_t)~(ICANON | ECHO | ISIG);
    raw.c_iflag &= (tcflag_t)~(IXON | ICRNL);
    raw.c_cc[VMIN] = 1;
    raw.c_cc[VTIME] = 0;
    if (tcsetattr(STDIN_FILENO, TCSAFLUSH, &raw) == -1) return 1;
    terminal_active = 1;
    signal(SIGTERM, on_signal);
    signal(SIGHUP, on_signal);
    fputs("\033[?1049h\033[?25l", stdout);
    int command_status = run_command("rfkill unblock wifi >/dev/null 2>&1");
    (void)command_status;
    command_status = run_command("nmcli radio wifi on >/dev/null 2>&1");
    (void)command_status;
    command_status = run_command("nmcli device set wlan0 managed yes >/dev/null 2>&1");
    (void)command_status;
    command_status = run_command("ip link set wlan0 up >/dev/null 2>&1");
    (void)command_status;
    begin_scan(&app);
    struct timespec next_refresh;
    clock_gettime(CLOCK_MONOTONIC, &next_refresh);
    int redraw = 1;
    for (;;) {
        if (redraw) {
            draw(&app);
            redraw = 0;
        }
        fd_set readfds;
        FD_ZERO(&readfds);
        FD_SET(STDIN_FILENO, &readfds);
        int maxfd = STDIN_FILENO;
        if (app.pty_fd >= 0) {
            FD_SET(app.pty_fd, &readfds);
            if (app.pty_fd > maxfd) maxfd = app.pty_fd;
        }
        if (app.scan_fd >= 0) {
            FD_SET(app.scan_fd, &readfds);
            if (app.scan_fd > maxfd) maxfd = app.scan_fd;
        }
        struct timeval timeout = {.tv_sec = 0, .tv_usec = 200000};
        int ready = select(maxfd + 1, &readfds, NULL, NULL, &timeout);
        if (ready > 0 && FD_ISSET(STDIN_FILENO, &readfds)) {
            handle_key(&app, read_key());
            redraw = 1;
        }
        if (app.pty_fd >= 0 && ready > 0 && FD_ISSET(app.pty_fd, &readfds)) {
            read_child_output(&app);
            redraw = 1;
        }
        if (app.scan_fd >= 0 && ready > 0 && FD_ISSET(app.scan_fd, &readfds)) {
            if (read_scan_output(&app)) redraw = 1;
        }
        if (app.child > 0) {
            int status = 0;
            pid_t ended = waitpid(app.child, &status, WNOHANG);
            if (ended == app.child) {
                finish_connection(&app, WIFEXITED(status) ? WEXITSTATUS(status) : 1);
                redraw = 1;
            }
        }
        if (app.scan_child > 0) {
            int status = 0;
            pid_t ended = waitpid(app.scan_child, &status, WNOHANG);
            if (ended == app.scan_child) {
                finish_scan(&app, WIFEXITED(status) ? WEXITSTATUS(status) : 1);
                redraw = 1;
            }
        }
        struct timespec now;
        clock_gettime(CLOCK_MONOTONIC, &now);
        if (now.tv_sec > next_refresh.tv_sec || (now.tv_sec == next_refresh.tv_sec && now.tv_nsec >= next_refresh.tv_nsec)) {
            if (refresh_connection(&app)) redraw = 1;
            next_refresh.tv_sec = now.tv_sec;
            next_refresh.tv_nsec = now.tv_nsec + 1000000000L;
            if (next_refresh.tv_nsec >= 1000000000) {
                ++next_refresh.tv_sec;
                next_refresh.tv_nsec -= 1000000000;
            }
        }
    }
}

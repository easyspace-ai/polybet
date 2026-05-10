import { useEffect, useMemo, useState, type CSSProperties } from "react";
import type { PolybetProjectBootstrap } from "../../shared/polybet-project-config";

function readBootstrap(): PolybetProjectBootstrap {
  return window.desktopAPI.polybetBootstrap;
}

function PolybetProjectSetup() {
  const bootstrap = useMemo(() => readBootstrap(), []);
  const needsProjectSetup = bootstrap.needsProjectSetup;
  const proxyGateOnly =
    !needsProjectSetup &&
    (bootstrap.needsOutboundVerification || bootstrap.outboundProbeFailed);

  const errMsg =
    !bootstrap.project.ok ? bootstrap.project.error.message : null;

  const project = bootstrap.project.ok ? bootstrap.project.config : null;
  const draft = bootstrap.draftDefaults;

  const [showAdvanced, setShowAdvanced] = useState(() => !proxyGateOnly);

  const [databaseUrl, setDatabaseUrl] = useState(
    () => project?.databaseUrl ?? draft?.databaseUrl ?? "",
  );
  const [host, setHost] = useState(
    () => project?.host ?? draft?.host ?? "127.0.0.1",
  );
  const [port, setPort] = useState(() => project?.port ?? draft?.port ?? "7633");
  const [outboundProxyUrl, setOutboundProxyUrl] = useState(
    () => project?.outboundProxyUrl ?? draft?.outboundProxyUrl ?? "",
  );
  const [readOnlyMode, setReadOnlyMode] = useState(
    () => Boolean(project?.readOnlyMode ?? draft?.readOnlyMode),
  );
  const [logLevel, setLogLevel] = useState(
    () => project?.logLevel ?? draft?.logLevel ?? "info",
  );
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [verifying, setVerifying] = useState(false);
  const [verifyError, setVerifyError] = useState<string | null>(null);

  async function onVerify() {
    setVerifyError(null);
    setVerifying(true);
    const res = await window.desktopAPI.verifyPolymarketOutbound({
      outboundProxyUrl,
    });
    setVerifying(false);
    if (!res.ok) {
      setVerifyError(res.error);
    }
  }

  async function onSave() {
    setSaveError(null);
    setSaving(true);
    const body: Record<string, unknown> = {
      schemaVersion: 1,
      databaseUrl: databaseUrl.trim(),
      host: host.trim(),
      port: port.trim(),
      readOnlyMode,
      logLevel: logLevel.trim() || "info",
    };
    const p = outboundProxyUrl.trim();
    if (p) body.outboundProxyUrl = p;
    const res = await window.desktopAPI.savePolybetProjectConfig(body);
    setSaving(false);
    if (!res.ok) {
      setSaveError(res.errors.join("\n"));
      return;
    }
    await window.desktopAPI.relaunchApp();
  }

  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        fontFamily: "system-ui, sans-serif",
        padding: "24px",
        maxWidth: 560,
        margin: "0 auto",
      }}
    >
      <h1 style={{ fontSize: "1.5rem", marginBottom: "8px" }}>
        {proxyGateOnly ? "出站网络检查" : "Polybet 本地服务配置"}
      </h1>
      <p style={{ color: "#555", marginBottom: "16px", textAlign: "center" }}>
        配置保存在{" "}
        <code style={{ fontSize: "0.85em" }}>~/.polybet/polybet-project.json</code>
        。访问 Polymarket Gamma 必须通过可用代理（或可达网络）；验证通过后才会加载核心界面。
      </p>
      {proxyGateOnly ? (
        <p
          style={{
            color: "#555",
            marginBottom: "16px",
            textAlign: "center",
            fontSize: "0.95rem",
          }}
        >
          请确认 HTTP/HTTPS 代理地址正确，然后点击「验证 Polymarket 连接」。若刚修改过代理，也可先「保存并重启」再验证。
        </p>
      ) : null}
      {errMsg ? (
        <p
          style={{
            color: "#b00020",
            marginBottom: "16px",
            textAlign: "center",
            fontSize: "0.95rem",
          }}
        >
          {errMsg}
        </p>
      ) : null}
      {bootstrap.outboundProbeFailed && bootstrap.outboundProbeError ? (
        <p
          style={{
            color: "#b00020",
            marginBottom: "16px",
            textAlign: "center",
            fontSize: "0.9rem",
          }}
        >
          上次启动未能访问 Polymarket：{bootstrap.outboundProbeError}
        </p>
      ) : null}
      {saveError ? (
        <pre
          style={{
            color: "#b00020",
            whiteSpace: "pre-wrap",
            marginBottom: "16px",
            fontSize: "0.85rem",
          }}
        >
          {saveError}
        </pre>
      ) : null}
      {verifyError ? (
        <pre
          style={{
            color: "#b00020",
            whiteSpace: "pre-wrap",
            marginBottom: "16px",
            fontSize: "0.85rem",
          }}
        >
          {verifyError}
        </pre>
      ) : null}

      <label style={labelStyle}>
        出站代理（HTTP_PLATFORM_PROXY_URL）
        <input
          style={inputStyle}
          value={outboundProxyUrl}
          onChange={(e) => setOutboundProxyUrl(e.target.value)}
          placeholder="http://127.0.0.1:7890"
        />
      </label>

      <div style={{ display: "flex", flexWrap: "wrap", gap: 12, marginTop: 8 }}>
        <button
          type="button"
          disabled={verifying || saving}
          onClick={() => void onVerify()}
          style={{
            padding: "12px 20px",
            background: verifying ? "#999" : "#0a7",
            color: "white",
            border: "none",
            borderRadius: "8px",
            cursor: verifying ? "default" : "pointer",
            fontSize: "1rem",
          }}
        >
          {verifying ? "验证中…" : "验证 Polymarket 连接"}
        </button>
        <button
          type="button"
          disabled={saving || verifying}
          onClick={() => void onSave()}
          style={{
            padding: "12px 20px",
            background: saving ? "#999" : "#0070f3",
            color: "white",
            border: "none",
            borderRadius: "8px",
            cursor: saving ? "default" : "pointer",
            fontSize: "1rem",
          }}
        >
          {saving ? "保存中…" : "保存并重启"}
        </button>
      </div>

      <button
        type="button"
        onClick={() => setShowAdvanced((v) => !v)}
        style={{
          marginTop: "20px",
          background: "none",
          border: "none",
          color: "#0070f3",
          cursor: "pointer",
          fontSize: "0.9rem",
          textDecoration: "underline",
        }}
      >
        {showAdvanced ? "收起高级设置" : "高级设置（数据库、监听地址）"}
      </button>

      {showAdvanced ? (
        <div style={{ width: "100%", marginTop: "16px" }}>
          <label style={labelStyle}>
            DATABASE_URL
            <input
              style={inputStyle}
              value={databaseUrl}
              onChange={(e) => setDatabaseUrl(e.target.value)}
              placeholder="file:./router.db?_pragma=foreign_keys(1)"
            />
          </label>
          <label style={labelStyle}>
            HOST
            <input
              style={inputStyle}
              value={host}
              onChange={(e) => setHost(e.target.value)}
            />
          </label>
          <label style={labelStyle}>
            PORT
            <input
              style={inputStyle}
              value={port}
              onChange={(e) => setPort(e.target.value)}
            />
          </label>
          <label style={labelStyle}>
            LOG_LEVEL
            <input
              style={inputStyle}
              value={logLevel}
              onChange={(e) => setLogLevel(e.target.value)}
            />
          </label>
          <label
            style={{
              ...labelStyle,
              flexDirection: "row",
              alignItems: "center",
              gap: 8,
            }}
          >
            <input
              type="checkbox"
              checked={readOnlyMode}
              onChange={(e) => setReadOnlyMode(e.target.checked)}
            />
            READ_ONLY_MODE
          </label>
          <p style={{ fontSize: "0.8rem", color: "#666", marginTop: 8 }}>
            开发环境默认 SQLite 在{" "}
            <code style={{ fontSize: "0.85em" }}>~/.polybet/embedded/</code>
            ；Go 侧会从同目录的{" "}
            <code style={{ fontSize: "0.85em" }}>polybet-project.json</code>{" "}
            补全尚未设置的环境变量。
          </p>
        </div>
      ) : null}
    </div>
  );
}

const labelStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  width: "100%",
  gap: 6,
  marginBottom: 14,
  fontSize: "0.85rem",
  fontWeight: 600,
};

const inputStyle: CSSProperties = {
  padding: "10px 12px",
  borderRadius: 6,
  border: "1px solid #ccc",
  fontSize: "0.95rem",
};

function AppContent() {
  const [serverStatus, setServerStatus] = useState<string>("Checking...");
  const [version, setVersion] = useState<string>("dev");

  useEffect(() => {
    const info = window.desktopAPI.appInfo as { version: string };
    setVersion(info?.version || "dev");

    void fetch("http://localhost:8080/health")
      .then((res) => res.json())
      .then((data: { status?: string }) =>
        setServerStatus(data.status ?? "unknown"),
      )
      .catch(() => setServerStatus("offline"));
  }, []);

  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        fontFamily: "system-ui, sans-serif",
        padding: "20px",
      }}
    >
      <h1 style={{ fontSize: "2.5rem", marginBottom: "20px" }}>🤖 polybet</h1>
      <p style={{ fontSize: "1.2rem", color: "#666" }}>Simple Desktop Demo</p>

      <div
        style={{
          marginTop: "40px",
          padding: "20px",
          background: "#f5f5f5",
          borderRadius: "8px",
          minWidth: "300px",
        }}
      >
        <h2 style={{ fontSize: "1rem", marginBottom: "15px" }}>Status</h2>
        <p>
          <strong>App Version:</strong> {version}
        </p>
        <p>
          <strong>Server:</strong> {serverStatus}
        </p>
      </div>

      <button
        type="button"
        onClick={() => {
          void window.desktopAPI.openExternal("https://polybet.ai");
        }}
        style={{
          marginTop: "20px",
          padding: "10px 20px",
          background: "#0070f3",
          color: "white",
          border: "none",
          borderRadius: "6px",
          cursor: "pointer",
          fontSize: "1rem",
        }}
      >
        Open External Link
      </button>
    </div>
  );
}

export default function App() {
  const bootstrap = readBootstrap();
  const blockSetup =
    bootstrap.needsProjectSetup ||
    bootstrap.needsOutboundVerification ||
    bootstrap.outboundProbeFailed;
  if (blockSetup) {
    return <PolybetProjectSetup />;
  }
  return <AppContent />;
}

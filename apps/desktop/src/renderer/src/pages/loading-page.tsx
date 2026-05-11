import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

interface StepStatus {
  status: string;
  error?: string;
  details?: {
    blocked?: boolean;
    country?: string;
    count?: number;
  };
}

interface InitStatus {
  configCheck: StepStatus;
  proxyCheck: StepStatus;
  balanceCache: StepStatus;
  positionCache: StepStatus;
  complete: boolean;
}

const initialStatus: InitStatus = {
  configCheck: { status: "pending" },
  proxyCheck: { status: "pending" },
  balanceCache: { status: "pending" },
  positionCache: { status: "pending" },
  complete: false,
};

const stepNames = [
  { key: "configCheck", name: "环境检测" },
  { key: "proxyCheck", name: "代理检测" },
  { key: "balanceCache", name: "余额缓存" },
  { key: "positionCache", name: "持仓缓存" },
];

function getStepIcon(status: string): string {
  switch (status) {
    case "done":
      return "✓";
    case "loading":
      return "◐";
    case "error":
      return "✗";
    default:
      return "○";
  }
}

function getStepColor(status: string): string {
  switch (status) {
    case "done":
      return "#22c55e";
    case "loading":
      return "#3b82f6";
    case "error":
      return "#ef4444";
    default:
      return "#9ca3af";
  }
}

export function LoadingPage() {
  const [status, setStatus] = useState<InitStatus>(initialStatus);
  const [showProxyModal, setShowProxyModal] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    const pollStatus = async () => {
      try {
        const res = await fetch("/api/setup/init-status");
        if (res.ok) {
          const data: InitStatus = await res.json();
          setStatus(data);

          if (data.complete) {
            navigate("/");
          }

          if (data.proxyCheck.status === "error" || (data.proxyCheck.details?.blocked === true)) {
            setShowProxyModal(true);
          }
        }
      } catch (err) {
        console.error("Failed to fetch init status:", err);
      }
    };

    pollStatus();
    const interval = setInterval(pollStatus, 2000);
    return () => clearInterval(interval);
  }, [navigate]);

  const completedSteps = stepNames.filter(
    (s) => status[s.key as keyof InitStatus]?.status === "done"
  ).length;
  const progress = (completedSteps / stepNames.length) * 100;

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
        background: "#fafafa",
      }}
    >
      <h1 style={{ fontSize: "1.5rem", marginBottom: "8px" }}>系统初始化中...</h1>
      <p style={{ color: "#666", marginBottom: "24px" }}>请稍候，正在准备您的环境</p>

      <div
        style={{
          width: "100%",
          maxWidth: "400px",
          background: "#e5e7eb",
          borderRadius: "8px",
          height: "8px",
          marginBottom: "32px",
        }}
      >
        <div
          style={{
            width: `${progress}%`,
            height: "100%",
            background: progress === 100 ? "#22c55e" : "#3b82f6",
            borderRadius: "8px",
            transition: "width 0.3s ease",
          }}
        />
      </div>

      <div style={{ width: "100%", maxWidth: "400px" }}>
        {stepNames.map((step) => {
          const stepStatus = status[step.key as keyof InitStatus] as StepStatus;
          const details = stepStatus?.details;
          let detailText = "";

          if (step.key === "proxyCheck" && details) {
            detailText = details.blocked ? `受限: ${details.country}` : `正常: ${details.country}`;
          } else if (step.key === "balanceCache" && details?.count !== undefined) {
            detailText = `${details.count} 个账号`;
          } else if (step.key === "positionCache" && details?.count !== undefined) {
            detailText = `${details.count} 个持仓`;
          }

          return (
            <div
              key={step.key}
              style={{
                display: "flex",
                alignItems: "center",
                padding: "12px 0",
                borderBottom: "1px solid #e5e7eb",
              }}
            >
              <span
                style={{
                  width: "24px",
                  height: "24px",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  borderRadius: "50%",
                  background: getStepColor(stepStatus?.status || "pending"),
                  color: "white",
                  fontSize: "12px",
                  marginRight: "12px",
                }}
              >
                {getStepIcon(stepStatus?.status || "pending")}
              </span>
              <div style={{ flex: 1 }}>
                <div style={{ fontWeight: 500 }}>{step.name}</div>
                {detailText && (
                  <div style={{ fontSize: "0.85rem", color: "#666" }}>{detailText}</div>
                )}
                {stepStatus?.error && (
                  <div style={{ fontSize: "0.85rem", color: "#ef4444" }}>{stepStatus.error}</div>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {status.complete && (
        <p style={{ marginTop: "24px", color: "#22c55e" }}>初始化完成，正在进入...</p>
      )}

      {showProxyModal && (
        <div
          style={{
            position: "fixed",
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: "rgba(0,0,0,0.5)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <div
            style={{
              background: "white",
              padding: "24px",
              borderRadius: "12px",
              maxWidth: "400px",
              width: "90%",
            }}
          >
            <h2 style={{ fontSize: "1.25rem", marginBottom: "16px" }}>代理配置问题</h2>
            <p style={{ color: "#666", marginBottom: "16px" }}>
              代理检测失败或被限制。请检查您的代理设置后重试。
            </p>
            <p style={{ fontSize: "0.85rem", color: "#666", marginBottom: "24px" }}>
              可以在设置中修改 HTTP_PLATFORM_PROXY_URL 配置。
            </p>
            <button
              onClick={() => setShowProxyModal(false)}
              style={{
                padding: "8px 16px",
                background: "#3b82f6",
                color: "white",
                border: "none",
                borderRadius: "6px",
                cursor: "pointer",
              }}
            >
              关闭
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export function VenueLogo({ size = 16 }: { size?: number }) {
  return (
    <span className="inline-flex items-center justify-center" style={{ width: size, height: size }}>
      <img
        src="/icon-white.png"
        alt="POLY"
        style={{ width: size * 1.6, height: size * 1.6, objectFit: 'contain' }}
      />
    </span>
  );
}

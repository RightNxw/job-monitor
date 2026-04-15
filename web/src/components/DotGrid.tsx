"use client";

export default function DotGrid({
  color = "rgba(255,255,255,0.15)",
  spacing = 24,
  size = 2,
  className = "",
}: {
  color?: string;
  spacing?: number;
  size?: number;
  className?: string;
}) {
  return (
    <div
      className={`pointer-events-none absolute inset-0 ${className}`}
      style={{
        backgroundImage: `radial-gradient(circle, ${color} ${size}px, transparent ${size}px)`,
        backgroundSize: `${spacing}px ${spacing}px`,
      }}
    />
  );
}

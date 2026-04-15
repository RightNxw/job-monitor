"use client";

import dynamic from "next/dynamic";
import Image from "next/image";
import DotGrid from "@/components/DotGrid";

const Globe = dynamic(() => import("@/components/Globe"), { ssr: false });

export default function Home() {
  return (
    <div className="h-screen w-screen overflow-hidden bg-black p-2 sm:p-3">
      <div className="relative h-full w-full rounded-2xl bg-[#080810] border border-white/[0.06] overflow-hidden flex flex-col">
        <DotGrid color="rgba(255,255,255,0.03)" spacing={24} size={1.5} />

        {/* ── LOGO TOP-LEFT ── */}
        <div className="relative z-10 px-6 sm:px-10 py-5 flex items-center justify-between">
          <Image
            src="/logo.png"
            alt="VSAT"
            width={676}
            height={369}
            priority
            className="h-8 sm:h-10 w-auto object-contain invert"
          />
          <a
            href="/dashboard"
            className="px-4 py-2 bg-white text-black text-[11px] font-semibold tracking-[0.1em] rounded hover:bg-white/90 transition-colors"
          >
            DASHBOARD
          </a>
        </div>

        {/* ── CENTER ── */}
        <div className="relative z-10 flex-1 flex flex-col items-center justify-center">
          <h1 className="text-3xl sm:text-4xl lg:text-6xl font-bold leading-[1.05] tracking-[-0.03em] text-white text-center mb-5">
            Find internships
            <br />
            before anyone else.
          </h1>
          <p className="text-xs sm:text-sm text-white/30 text-center max-w-md mb-8 leading-relaxed">
            Real-time monitoring across 50+ companies. Job postings, interview intel, and hiring signals the second they appear.
          </p>

          <div className="w-56 h-56 sm:w-72 sm:h-72 lg:w-[380px] lg:h-[380px]">
            <Globe className="w-full h-full" />
          </div>
        </div>

        {/* ── BOTTOM ── */}
        <div className="relative z-10 flex items-center justify-end px-6 sm:px-10 py-4 border-t border-white/[0.04] text-[9px] tracking-[0.15em] text-white/15">
          <span>VSAT &copy; 2026</span>
        </div>
      </div>
    </div>
  );
}

"use client";

import { useEffect, useState } from "react";
import { supabase } from "@/lib/supabase";
import { timeAgoShort } from "@/lib/time";
import Link from "next/link";

interface Signal {
  id: number;
  company: string;
  event_type: string;
  content: string;
  role: string;
  team: string;
  location: string;
  questions: string;
  round: string;
  timeline: string;
  discord_user: string;
  channel: string;
  created_at: string;
}

const eventColors: Record<string, string> = {
  apps_open: "bg-green-500/15 text-green-400",
  oa_sent: "bg-yellow-500/15 text-yellow-400",
  interviewing: "bg-orange-500/15 text-orange-400",
  offering: "bg-blue-500/15 text-blue-400",
  rejecting: "bg-red-500/15 text-red-400",
  lc_question: "bg-purple-500/15 text-purple-400",
  closed: "bg-white/10 text-white/40",
  general: "bg-white/5 text-white/30",
};

const eventLabels: Record<string, string> = {
  apps_open: "Apps Open",
  oa_sent: "OA",
  interviewing: "Interview",
  offering: "Offer",
  rejecting: "Rejection",
  lc_question: "LC Question",
  closed: "Closed",
  general: "News",
};

function sourceIcon(channel: string) {
  if (channel === "instagram") return "📷";
  if (channel.startsWith("r/")) return "🟠";
  return "💬";
}

export default function Feed() {
  const [signals, setSignals] = useState<Signal[]>([]);
  const [loading, setLoading] = useState(true);
  const [sourceFilter, setSourceFilter] = useState("");
  const [eventFilter, setEventFilter] = useState("");

  useEffect(() => {
    async function fetch() {
      let query = supabase
        .from("company_intel")
        .select("*")
        .order("created_at", { ascending: false })
        .limit(100);

      if (sourceFilter) {
        query = query.ilike("channel", `%${sourceFilter}%`);
      }
      if (eventFilter) {
        query = query.eq("event_type", eventFilter);
      }

      const { data, error } = await query;
      if (!error && data) setSignals(data);
      setLoading(false);
    }
    fetch();
  }, [sourceFilter, eventFilter]);

  const sources = [...new Set(signals.map((s) => s.channel))].sort();
  const events = [...new Set(signals.map((s) => s.event_type))].sort();

  return (
    <div>
      <div className="mb-6">
        <h1 className="text-2xl font-bold mb-1">Signals</h1>
        <p className="text-white/35 text-sm">
          Hiring signals from Discord, Reddit, and Instagram.
        </p>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-2 mb-5">
        <select
          value={sourceFilter}
          onChange={(e) => setSourceFilter(e.target.value)}
          className="bg-white/[0.04] border border-white/[0.06] rounded-md px-3 py-2 text-sm text-white/60 focus:outline-none cursor-pointer"
        >
          <option value="">All Sources</option>
          {sources.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <select
          value={eventFilter}
          onChange={(e) => setEventFilter(e.target.value)}
          className="bg-white/[0.04] border border-white/[0.06] rounded-md px-3 py-2 text-sm text-white/60 focus:outline-none cursor-pointer"
        >
          <option value="">All Events</option>
          {events.map((e) => (
            <option key={e} value={e}>{eventLabels[e] || e}</option>
          ))}
        </select>
        <span className="text-white/25 text-xs ml-auto">{signals.length} signals</span>
      </div>

      {loading ? (
        <div className="text-white/30 text-center py-20 text-sm">Loading...</div>
      ) : signals.length === 0 ? (
        <div className="text-white/30 text-center py-20 text-sm">
          No signals yet. Data flows in from Discord, Reddit, and Instagram monitoring.
        </div>
      ) : (
        <div className="space-y-1">
          {signals.map((s) => (
            <div
              key={s.id}
              className="flex items-start gap-3 px-4 py-3 rounded-lg hover:bg-white/[0.02] transition-colors"
            >
              {/* Source icon */}
              <span className="text-base mt-0.5 shrink-0">{sourceIcon(s.channel)}</span>

              {/* Content */}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 mb-1">
                  <Link
                    href={`/company/${encodeURIComponent(s.company)}`}
                    className="text-sm font-medium text-white/85 hover:text-white transition-colors"
                  >
                    {s.company}
                  </Link>
                  <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${eventColors[s.event_type] || eventColors.general}`}>
                    {eventLabels[s.event_type] || s.event_type}
                  </span>
                  {s.role && <span className="text-xs text-white/35">{s.role}</span>}
                </div>

                <p className="text-sm text-white/50 leading-relaxed line-clamp-2">
                  {s.content}
                </p>

                {s.questions && (
                  <div className="mt-1.5 text-xs text-purple-400/70 bg-purple-500/10 rounded px-2 py-1 inline-block">
                    {s.questions}
                  </div>
                )}

                <div className="flex items-center gap-3 mt-1.5 text-[11px] text-white/20">
                  <span>{s.discord_user}</span>
                  <span>{s.channel}</span>
                  {s.location && <span>{s.location}</span>}
                  {s.round && <span>{s.round}</span>}
                  {s.timeline && <span>{s.timeline}</span>}
                </div>
              </div>

              {/* Time */}
              <span className="text-[11px] text-white/20 shrink-0 mt-1">{timeAgoShort(s.created_at)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

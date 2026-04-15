"use client";

import { useEffect, useState } from "react";
import { supabase } from "@/lib/supabase";
import Link from "next/link";

interface Interview {
  id: number;
  company: string;
  role: string;
  stage: string;
  questions: string;
  difficulty: string;
  outcome: string;
  timeline: string;
  location: string;
  source: string;
  source_url: string;
  author: string;
  content: string;
  created_at: string;
}

const stageBadge: Record<string, string> = {
  oa_sent: "bg-yellow-500/20 text-yellow-400",
  interviewing: "bg-orange-500/20 text-orange-400",
  offering: "bg-blue-500/20 text-blue-400",
  rejecting: "bg-red-500/20 text-red-400",
  lc_question: "bg-purple-500/20 text-purple-400",
};

const stageLabel: Record<string, string> = {
  oa_sent: "OA",
  interviewing: "Interview",
  offering: "Offer",
  rejecting: "Rejection",
  lc_question: "LC Question",
};

export default function Interviews() {
  const [reports, setReports] = useState<Interview[]>([]);
  const [loading, setLoading] = useState(true);
  const [companyFilter, setCompanyFilter] = useState("");

  useEffect(() => {
    async function fetch() {
      let query = supabase
        .from("interview_reports")
        .select("*")
        .order("created_at", { ascending: false })
        .limit(100);

      if (companyFilter) {
        query = query.ilike("company", `%${companyFilter}%`);
      }

      const { data, error } = await query;
      if (!error && data) {
        setReports(data);
      }
      setLoading(false);
    }
    fetch();
  }, [companyFilter]);

  const companies = [...new Set(reports.map((r) => r.company))].sort();

  return (
    <div>
      <div className="mb-8">
        <h1 className="text-2xl font-bold mb-1">Interviews</h1>
        <p className="text-white/40 text-sm">
          OAs, phone screens, onsites, and questions asked. From Discord and Reddit.
        </p>
      </div>

      <div className="flex gap-3 mb-6">
        <select
          value={companyFilter}
          onChange={(e) => setCompanyFilter(e.target.value)}
          className="bg-white/[0.04] border border-white/[0.08] rounded-lg px-4 py-2.5 text-sm text-white/70 focus:outline-none"
        >
          <option value="">All Companies</option>
          {companies.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>
      </div>

      {loading ? (
        <div className="text-white/30 text-center py-20">Loading...</div>
      ) : reports.length === 0 ? (
        <div className="text-white/30 text-center py-20">
          No interview reports yet. Data flows in from Discord and Reddit monitoring.
        </div>
      ) : (
        <div className="space-y-3">
          {reports.map((r) => (
            <div
              key={r.id}
              className="bg-white/[0.02] border border-white/[0.06] rounded-lg px-5 py-4"
            >
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 mb-2">
                    <Link
                      href={`/company/${encodeURIComponent(r.company)}`}
                      className="text-sm font-medium text-white/90 hover:text-white hover:underline"
                    >
                      {r.company}
                    </Link>
                    {r.role && <span className="text-xs text-white/40">{r.role}</span>}
                    <span
                      className={`px-2 py-0.5 rounded text-[10px] font-medium ${stageBadge[r.stage] || "bg-white/10 text-white/40"}`}
                    >
                      {stageLabel[r.stage] || r.stage}
                    </span>
                  </div>

                  <p className="text-sm text-white/60 leading-relaxed line-clamp-3">
                    {r.content}
                  </p>

                  {r.questions && (
                    <div className="mt-2 text-xs text-purple-400/80 bg-purple-500/10 rounded px-2.5 py-1.5 inline-block">
                      {r.questions}
                    </div>
                  )}

                  <div className="flex items-center gap-3 mt-2.5 text-[11px] text-white/30">
                    <span>{r.author}</span>
                    <span>{r.source}</span>
                    {r.location && <span>{r.location}</span>}
                    {r.timeline && <span>{r.timeline}</span>}
                  </div>
                </div>
                <div className="text-[11px] text-white/20 whitespace-nowrap">
                  {new Date(r.created_at).toLocaleDateString()}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

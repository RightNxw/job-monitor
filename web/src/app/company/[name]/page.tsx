"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { supabase } from "@/lib/supabase";
import Link from "next/link";

interface Job {
  id: number;
  title: string;
  location: string;
  url: string;
  source: string;
  discovered_at: string;
}

interface Interview {
  id: number;
  role: string;
  stage: string;
  questions: string;
  content: string;
  author: string;
  source: string;
  created_at: string;
}

interface Status {
  current_stage: string;
  signal_count: number;
  summary: string;
}

const stageColors: Record<string, string> = {
  apps_open: "bg-green-500/20 text-green-400",
  oa_sent: "bg-yellow-500/20 text-yellow-400",
  interviewing: "bg-orange-500/20 text-orange-400",
  offering: "bg-blue-500/20 text-blue-400",
  rejecting: "bg-red-500/20 text-red-400",
  closed: "bg-white/10 text-white/40",
};

const stageLabels: Record<string, string> = {
  apps_open: "Apps Open",
  oa_sent: "Sending OAs",
  interviewing: "Interviewing",
  offering: "Extending Offers",
  rejecting: "Rejecting",
  closed: "Closed",
};

export default function CompanyPage() {
  const params = useParams();
  const name = decodeURIComponent(params.name as string);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [interviews, setInterviews] = useState<Interview[]>([]);
  const [status, setStatus] = useState<Status | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function fetch() {
      const [jobsRes, interviewsRes, statusRes] = await Promise.all([
        supabase
          .from("jobs")
          .select("id, title, location, url, source, discovered_at")
          .ilike("company", name)
          .order("discovered_at", { ascending: false })
          .limit(20),
        supabase
          .from("interview_reports")
          .select("id, role, stage, questions, content, author, source, created_at")
          .ilike("company", name)
          .order("created_at", { ascending: false })
          .limit(20),
        supabase
          .from("company_status")
          .select("current_stage, signal_count, summary")
          .ilike("company", name)
          .single(),
      ]);

      setJobs(jobsRes.data || []);
      setInterviews(interviewsRes.data || []);
      if (statusRes.data) setStatus(statusRes.data);
      setLoading(false);
    }
    fetch();
  }, [name]);

  if (loading) {
    return <div className="min-h-screen bg-[#080810] text-white/30 flex items-center justify-center">Loading...</div>;
  }

  return (
    <div className="min-h-screen bg-[#080810] text-white">
      <nav className="border-b border-white/[0.06] px-6 py-4 flex items-center gap-4">
        <Link href="/dashboard" className="text-white/40 hover:text-white text-sm transition-colors">
          Dashboard
        </Link>
        <span className="text-white/20">/</span>
        <span className="text-sm font-medium">{name}</span>
      </nav>

      <div className="max-w-5xl mx-auto px-6 py-8">
        {/* Header */}
        <div className="mb-8">
          <div className="flex items-center gap-4 mb-2">
            <h1 className="text-3xl font-bold">{name}</h1>
            {status && (
              <span className={`px-3 py-1 rounded-md text-xs font-medium ${stageColors[status.current_stage] || "bg-white/5 text-white/30"}`}>
                {stageLabels[status.current_stage] || status.current_stage}
              </span>
            )}
          </div>
          {status && (
            <p className="text-sm text-white/40">{status.summary} ({status.signal_count} signals)</p>
          )}
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Job Postings */}
          <div>
            <h2 className="text-lg font-semibold mb-4">Open Positions ({jobs.length})</h2>
            {jobs.length === 0 ? (
              <div className="text-white/30 text-sm py-8 text-center bg-white/[0.02] rounded-lg border border-white/[0.06]">
                No internship postings found
              </div>
            ) : (
              <div className="space-y-2">
                {jobs.map((j) => (
                  <a
                    key={j.id}
                    href={j.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="block bg-white/[0.02] border border-white/[0.06] rounded-lg px-4 py-3 hover:bg-white/[0.04] hover:border-white/[0.1] transition-all"
                  >
                    <div className="text-sm font-medium text-white/90">{j.title}</div>
                    <div className="flex items-center gap-2 mt-1 text-xs text-white/40">
                      {j.location && <span>{j.location}</span>}
                      <span className="text-white/20">|</span>
                      <span>{j.source}</span>
                    </div>
                  </a>
                ))}
              </div>
            )}
          </div>

          {/* Interview Reports */}
          <div>
            <h2 className="text-lg font-semibold mb-4">Interview Reports ({interviews.length})</h2>
            {interviews.length === 0 ? (
              <div className="text-white/30 text-sm py-8 text-center bg-white/[0.02] rounded-lg border border-white/[0.06]">
                No interview reports yet
              </div>
            ) : (
              <div className="space-y-2">
                {interviews.map((r) => (
                  <div
                    key={r.id}
                    className="bg-white/[0.02] border border-white/[0.06] rounded-lg px-4 py-3"
                  >
                    <div className="flex items-center gap-2 mb-1.5">
                      {r.role && <span className="text-xs text-white/60">{r.role}</span>}
                      <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${stageColors[r.stage] || "bg-white/5 text-white/30"}`}>
                        {stageLabels[r.stage] || r.stage}
                      </span>
                    </div>
                    <p className="text-sm text-white/50 line-clamp-2">{r.content}</p>
                    {r.questions && (
                      <div className="mt-1.5 text-xs text-purple-400/80 bg-purple-500/10 rounded px-2 py-1 inline-block">
                        {r.questions}
                      </div>
                    )}
                    <div className="text-[11px] text-white/25 mt-1.5">
                      {r.author} via {r.source}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

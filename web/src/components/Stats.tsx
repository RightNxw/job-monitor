"use client";

import { useEffect, useState } from "react";
import { supabase } from "@/lib/supabase";

interface StatsData {
  totalJobs: number;
  totalCompanies: number;
  totalSignals: number;
  totalInterviews: number;
  recentJobs: number;
}

export default function Stats() {
  const [stats, setStats] = useState<StatsData | null>(null);

  useEffect(() => {
    async function fetch() {
      const [jobsRes, companiesRes, signalsRes, interviewsRes, recentRes] = await Promise.all([
        supabase.from("jobs").select("id", { count: "exact", head: true }),
        supabase.from("jobs").select("company").limit(10000),
        supabase.from("company_intel").select("id", { count: "exact", head: true }),
        supabase.from("interview_reports").select("id", { count: "exact", head: true }),
        supabase.from("jobs").select("id", { count: "exact", head: true }).gte(
          "discovered_at",
          new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString()
        ),
      ]);

      const uniqueCompanies = new Set((companiesRes.data || []).map((r: { company: string }) => r.company));

      setStats({
        totalJobs: jobsRes.count || 0,
        totalCompanies: uniqueCompanies.size,
        totalSignals: signalsRes.count || 0,
        totalInterviews: interviewsRes.count || 0,
        recentJobs: recentRes.count || 0,
      });
    }
    fetch();
  }, []);

  if (!stats) return null;

  const items = [
    { label: "Total Postings", value: stats.totalJobs.toLocaleString() },
    { label: "Companies", value: stats.totalCompanies.toLocaleString() },
    { label: "New Today", value: stats.recentJobs.toLocaleString() },
    { label: "Hiring Signals", value: stats.totalSignals.toLocaleString() },
    { label: "Interview Reports", value: stats.totalInterviews.toLocaleString() },
  ];

  return (
    <div className="grid grid-cols-2 sm:grid-cols-5 gap-3 mb-8">
      {items.map((item) => (
        <div
          key={item.label}
          className="bg-white/[0.02] border border-white/[0.06] rounded-lg px-4 py-3 text-center"
        >
          <div className="text-xl font-bold text-white/90">{item.value}</div>
          <div className="text-[11px] text-white/35 mt-0.5">{item.label}</div>
        </div>
      ))}
    </div>
  );
}

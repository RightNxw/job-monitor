"use client";

import { useEffect, useState } from "react";
import { supabase } from "@/lib/supabase";
import Link from "next/link";

interface CompanyHeat {
  company: string;
  current_stage: string;
  signal_count: number;
  summary: string;
}

const stageColors: Record<string, string> = {
  apps_open: "bg-green-500/20 text-green-400 border-green-500/30",
  oa_sent: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
  interviewing: "bg-orange-500/20 text-orange-400 border-orange-500/30",
  offering: "bg-blue-500/20 text-blue-400 border-blue-500/30",
  rejecting: "bg-red-500/20 text-red-400 border-red-500/30",
  closed: "bg-white/10 text-white/40 border-white/20",
  unknown: "bg-white/5 text-white/30 border-white/10",
};

const stageLabels: Record<string, string> = {
  apps_open: "Apps Open",
  oa_sent: "Sending OAs",
  interviewing: "Interviewing",
  offering: "Extending Offers",
  rejecting: "Rejecting",
  closed: "Closed",
  unknown: "Unknown",
};

export default function HeatMap() {
  const [companies, setCompanies] = useState<CompanyHeat[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function fetch() {
      const { data, error } = await supabase
        .from("company_status")
        .select("*")
        .order("signal_count", { ascending: false });

      if (!error && data) {
        setCompanies(data);
      }
      setLoading(false);
    }
    fetch();
  }, []);

  const grouped = companies.reduce(
    (acc, c) => {
      const stage = c.current_stage || "unknown";
      if (!acc[stage]) acc[stage] = [];
      acc[stage].push(c);
      return acc;
    },
    {} as Record<string, CompanyHeat[]>
  );

  const stageOrder = ["apps_open", "oa_sent", "interviewing", "offering", "rejecting", "closed", "unknown"];

  return (
    <div>
      <div className="mb-8">
        <h1 className="text-2xl font-bold mb-1">Pipeline</h1>
        <p className="text-white/40 text-sm">
          Where companies are in their hiring pipeline right now.
        </p>
      </div>

      {loading ? (
        <div className="text-white/30 text-center py-20">Loading...</div>
      ) : companies.length === 0 ? (
        <div className="text-white/30 text-center py-20">
          No signals yet. The monitor needs to run and collect data from Discord/Reddit first.
        </div>
      ) : (
        <div className="space-y-8">
          {stageOrder.map((stage) => {
            const items = grouped[stage];
            if (!items || items.length === 0) return null;
            return (
              <div key={stage}>
                <div className="flex items-center gap-3 mb-3">
                  <span
                    className={`px-2.5 py-1 rounded-md text-xs font-medium border ${stageColors[stage] || stageColors.unknown}`}
                  >
                    {stageLabels[stage] || stage}
                  </span>
                  <span className="text-xs text-white/30">{items.length} companies</span>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                  {items.map((c) => (
                    <Link
                      key={c.company}
                      href={`/company/${encodeURIComponent(c.company)}`}
                      className="bg-white/[0.02] border border-white/[0.06] rounded-lg px-4 py-3 hover:bg-white/[0.04] hover:border-white/[0.1] transition-all"
                    >
                      <div className="font-medium text-sm text-white/90">{c.company}</div>
                      <div className="text-xs text-white/40 mt-1">{c.summary}</div>
                      <div className="text-[11px] text-white/25 mt-1.5">{c.signal_count} signals</div>
                    </Link>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

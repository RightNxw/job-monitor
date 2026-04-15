"use client";

import { useEffect, useState } from "react";
import { supabase } from "@/lib/supabase";
import { timeAgo } from "@/lib/time";
import Link from "next/link";
import Stats from "@/components/Stats";

interface Job {
  id: number;
  source: string;
  title: string;
  company: string;
  location: string;
  url: string;
  status: string;
  discovered_at: string;
  posted_at: string;
}

export default function Dashboard() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [sourceFilter, setSourceFilter] = useState("");
  const [companyFilter, setCompanyFilter] = useState("");
  const [sortBy, setSortBy] = useState<"date" | "company">("date");

  useEffect(() => {
    async function fetchJobs() {
      let query = supabase
        .from("jobs")
        .select("*")
        .order("discovered_at", { ascending: false })
        .limit(200);

      if (sourceFilter) {
        query = query.ilike("source", `%${sourceFilter}%`);
      }

      const { data, error } = await query;
      if (error) {
        console.error("Error fetching jobs:", error);
      } else {
        setJobs(data || []);
      }
      setLoading(false);
    }
    fetchJobs();
  }, [sourceFilter]);

  const filtered = jobs
    .filter((j) => {
      if (companyFilter && j.company !== companyFilter) return false;
      if (!search) return true;
      const q = search.toLowerCase();
      return (
        j.title.toLowerCase().includes(q) ||
        j.company.toLowerCase().includes(q) ||
        j.location.toLowerCase().includes(q)
      );
    })
    .sort((a, b) => {
      if (sortBy === "company") return a.company.localeCompare(b.company);
      return 0; // already sorted by date from supabase
    });

  const sources = [...new Set(jobs.map((j) => j.source.split(":")[0]))];
  const companies = [...new Set(jobs.map((j) => j.company))].sort();

  function clearFilters() {
    setSearch("");
    setSourceFilter("");
    setCompanyFilter("");
  }

  const hasFilters = search || sourceFilter || companyFilter;

  return (
    <div>
      <Stats />

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-2 mb-5">
        <div className="relative">
          <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-white/30" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            type="text"
            placeholder="Search..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-56 bg-white/[0.04] border border-white/[0.06] rounded-md pl-9 pr-3 py-2 text-sm text-white placeholder-white/25 focus:outline-none focus:border-white/15 transition-colors"
          />
        </div>
        <select
          value={sourceFilter}
          onChange={(e) => setSourceFilter(e.target.value)}
          className="bg-white/[0.04] border border-white/[0.06] rounded-md px-3 py-2 text-sm text-white/60 focus:outline-none cursor-pointer"
        >
          <option value="">Source</option>
          {sources.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <select
          value={companyFilter}
          onChange={(e) => setCompanyFilter(e.target.value)}
          className="bg-white/[0.04] border border-white/[0.06] rounded-md px-3 py-2 text-sm text-white/60 focus:outline-none cursor-pointer"
        >
          <option value="">Company</option>
          {companies.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>
        <select
          value={sortBy}
          onChange={(e) => setSortBy(e.target.value as "date" | "company")}
          className="bg-white/[0.04] border border-white/[0.06] rounded-md px-3 py-2 text-sm text-white/60 focus:outline-none cursor-pointer"
        >
          <option value="date">Newest</option>
          <option value="company">Company A-Z</option>
        </select>
        {hasFilters && (
          <button
            onClick={clearFilters}
            className="text-xs text-white/40 hover:text-white/70 transition-colors px-2 py-1"
          >
            Clear
          </button>
        )}
        <span className="text-white/25 text-xs ml-auto tabular-nums">
          {filtered.length} of {jobs.length}
        </span>
      </div>

      {/* Table */}
      {loading ? (
        <div className="text-white/30 text-center py-20 text-sm">Loading...</div>
      ) : filtered.length === 0 ? (
        <div className="text-white/30 text-center py-20 text-sm">
          {hasFilters ? "No jobs match your filters" : "No jobs found"}
        </div>
      ) : (
        <div className="border border-white/[0.06] rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-white/[0.03] text-white/50 text-xs uppercase tracking-wider">
                <th className="text-left px-4 py-3 font-medium">Company</th>
                <th className="text-left px-4 py-3 font-medium">Position</th>
                <th className="text-left px-4 py-3 font-medium hidden md:table-cell">Location</th>
                <th className="text-left px-4 py-3 font-medium w-24">Posted</th>
                <th className="text-right px-4 py-3 font-medium w-20"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.04]">
              {filtered.map((job) => (
                <tr
                  key={job.id}
                  className="hover:bg-white/[0.02] transition-colors"
                >
                  <td className="px-4 py-3 whitespace-nowrap">
                    <Link
                      href={`/company/${encodeURIComponent(job.company)}`}
                      className="text-white/80 hover:text-white transition-colors"
                    >
                      {job.company}
                    </Link>
                    <div className="text-[10px] text-white/20 mt-0.5">{job.source}</div>
                  </td>
                  <td className="px-4 py-3 text-white/70 max-w-sm">
                    <div className="truncate">{job.title}</div>
                  </td>
                  <td className="px-4 py-3 text-white/40 max-w-xs truncate hidden md:table-cell">
                    {job.location || "\u2014"}
                  </td>
                  <td className="px-4 py-3 text-white/30 whitespace-nowrap text-xs">
                    {job.discovered_at ? timeAgo(job.discovered_at) : "\u2014"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    {job.url && (
                      <a
                        href={job.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 text-xs text-white/50 hover:text-white border border-white/[0.08] hover:border-white/20 rounded px-2.5 py-1 transition-all"
                      >
                        Apply
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                        </svg>
                      </a>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

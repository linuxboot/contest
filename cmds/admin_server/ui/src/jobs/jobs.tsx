import React, { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Table, useToaster, Message } from 'rsuite';
import { Column, Cell, HeaderCell } from 'rsuite-table';
import { getJobs, Job } from '../api/tags';
import { TypeAttributes } from 'rsuite/esm/@types/common';
import DateCell from '../date_cell/date_cell';

export default function Jobs() {
    const [loading, setLoading] = useState<boolean>(false);
    const [jobs, setJobs] = useState<Job[]>([]);
    const { name } = useParams();
    const toaster = useToaster();

    const showMsg = (type: TypeAttributes.Status, message: string) => {
        toaster.push(
            <Message showIcon type={type}>
                {message}
            </Message>,
            { placement: 'topEnd' }
        );
    };

    useEffect(() => {
        (async () => {
            setLoading(true);
            try {
                let result = await getJobs(name || '');
                setJobs(result || []);
            } catch (err) {
                showMsg('error', err?.message);
            }
            setLoading(false);
        })();
    }, []);

    return (
        <div>
            <Table
                loading={loading}
                height={700}
                data={jobs ?? []}
                wordWrap="break-word"
                rowHeight={30}
            >
                <Column width={80} align="center" fixed>
                    <HeaderCell>Job ID</HeaderCell>
                    <Cell className="log-table__cell" dataKey="job_id" />
                </Column>
                <Column width={250} align="center" fixed>
                    <HeaderCell>Report Time</HeaderCell>
                    <DateCell
                        className="log-table__cell"
                        dataKey="report.time"
                    />
                </Column>
                <Column width={80} align="center" fixed>
                    <HeaderCell>Success</HeaderCell>
                    <Cell
                        className="log-table__cell"
                        dataKey="report.success"
                    />
                </Column>
                <Column width={120} align="center" fixed>
                    <HeaderCell>Reporter Name</HeaderCell>
                    <Cell className="log-table__cell" dataKey="report.name" />
                </Column>
                <Column width={600} align="left" flexGrow={1}>
                    <HeaderCell>Data</HeaderCell>
                    <Cell className="log-table__cell" dataKey="report.data" />
                </Column>
            </Table>
        </div>
    );
}

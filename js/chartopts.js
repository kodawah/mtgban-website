function getChartOpts() {
    return {
        responsive: true,
        // Keep "holes" in the graph when data is missing
        spanGaps: false,
        legend: {
            position: "top",
            align: "center",
        },
        // Speed up initial animation
        animation: {
            duration: 1000,
        },
        // The labels appearing on top of points
        tooltips: {
            mode: "index",
            position: "nearest",
            intersect: false,
            callbacks: {
                // Make sure there is a $ and floating point looks sane
                label: function(tooltipItems, data) {
                    return data.datasets[tooltipItems.datasetIndex].label + ': $' + parseFloat(data.datasets[tooltipItems.datasetIndex].data[tooltipItems.index]).toFixed(2);
                },
            },
        },
        // The animation when hovering on an axis
        hover: {
            mode: "x-axis",
            intersect: true,
        },
        scales: {
            xAxes: [
                {
                    display: true,
                    type: "time",
                    distribution: "linear",
                    time: {
                        unit: "day",
                    },
                },
            ],
            yAxes: [
                {
                    display: true,
                    ticks: {
                        beginAtZero: true,
                        callback: function(value, index, values) {
                            return '$' + value.toFixed(2);
                        },
                    },
                    afterDataLimits: function(axis) {
                        // Keep a 10% buffer
                        axis.max *= 1.1;
                    },
                },
            ],
        },
    }
}

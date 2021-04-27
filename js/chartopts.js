// Custom chart type which copies the "line" one and draws
// an extra vertical line on hover
Chart.defaults.banWithLine = Chart.defaults.line;
Chart.controllers.banWithLine = Chart.controllers.line.extend({
    draw: function(ease) {
        Chart.controllers.line.prototype.draw.call(this, ease);

        if (this.chart.tooltip._active && this.chart.tooltip._active.length) {
            var activePoint = this.chart.tooltip._active[0];
            var ctx = this.chart.ctx;
            var x = activePoint.tooltipPosition().x;
            var topY = this.chart.legend.bottom;
            var bottomY = this.chart.chartArea.bottom;

            // Draw vertical line
            ctx.save();
            ctx.beginPath();
            ctx.moveTo(x, topY);
            ctx.lineTo(x, bottomY);
            ctx.lineWidth = 2;
            ctx.strokeStyle = '#92929242';
            ctx.stroke();
            ctx.restore();
        }
    }
});

// Custom positioner to draw the tooltip on the bottom
Chart.Tooltip.positioners.bottom = function(elements, position) {
    if (!elements.length) {
        return false;
    }
    return {
        x: elements[0]._view.x,
        y: this._chart.chartArea.bottom,
    }
};

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
            position: "bottom",
            backgroundColor: 'rgba(10, 10, 10, 220)',
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
            animationDuration: 0,
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

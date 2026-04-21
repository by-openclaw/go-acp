import { BootstrapService } from '../../../../src/bootstrap.service';
import { MaintenanceMessageCommandParams } from '../../../../src/command/rx/007-maintenance-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/rx/007-maintenance-message/params.validator';

describe('Maintenance Message - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };

            // Act
            const validator = new CommandParamsValidator(params);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });
    describe('validate', () => {
        it('Should succeed with valid params - HARD RESET', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 0,
                levelId: 0
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });
        it('Should succeed with valid params - SOFT RESET', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 19,
                levelId: 15
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });
        it('Should succeed with valid params - DATABASE TRANSFERT', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 18,
                levelId: 14
            };
            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });
        it('Should succeed with valid params - CLEAR PROTECTS', () => {
            // Arrange
            const params: MaintenanceMessageCommandParams = {
                matrixId: 0xff,
                levelId: 0xff
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });
    });
});

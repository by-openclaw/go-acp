import { BootstrapService } from "../../../../src/bootstrap.service";
import { CrossPointTallyMessageCommandParams } from "../../../../src/command/tx/003-crosspoint-tally-message/params";
import { CommandParamsValidator } from "../../../../src/command/tx/003-crosspoint-tally-message/params.validator";
import { LocaleData } from "../../../../src/common/locale-data/locale-data.model";
import { CommandErrorsKeys } from "../../../../src/command/locale-data-keys";


describe('CrossPoint Tally Message Command - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 0,
                statusId: 0
            };
            // Act
            const validator = new CommandParamsValidator(params);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params', () => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: 1,
                levelId: 1,
                destinationId: 1,
                sourceId: 1,
                statusId: 1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                sourceId: -1,
                statusId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceId).toBeDefined();
            expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: CrossPointTallyMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                sourceId: 65536,
                statusId: 0
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.destinationId).toBeDefined();
            expect(errors.destinationId.id).toBe(CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceId).toBeDefined();
            expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});

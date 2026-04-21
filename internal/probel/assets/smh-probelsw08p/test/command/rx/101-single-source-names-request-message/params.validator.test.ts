import { BootstrapService } from '../../../../src/bootstrap.service';
import { SingleSourceNamesRequestMessageCommandParams } from '../../../../src/command/rx/101-single-source-names-request-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/rx/101-single-source-names-request-message/params.validator';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Single Source Names Request Message Command - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                sourceId: 0
            };

            // Act
            const validator = new CommandParamsValidator(params);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });
    
    describe('validate', () => {
        it('Should succeed with valid params - ...', () => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: 1,
                levelId: 1,
                sourceId: 1
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
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                sourceId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceId).toBeDefined();
            expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                sourceId: 65536
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.matrixId).toBeDefined();
            expect(errors.matrixId.id).toBe(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.levelId).toBeDefined();
            expect(errors.levelId.id).toBe(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.sourceId).toBeDefined();
            expect(errors.sourceId.id).toBe(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
